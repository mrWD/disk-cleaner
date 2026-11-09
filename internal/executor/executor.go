// Package executor — настоящее выполнение действий: удаление папок,
// запуск команд, запрос пароля администратора.
//
// В core живёт только интерфейс core.Executor (порт), а здесь — его
// реализация (адаптер). Тесты ядра обходятся фейком и ничего не удаляют.
//
// Здесь же последняя линия обороны: даже если провайдер с ошибкой попросит
// удалить домашнюю папку или ~/Library, исполнитель откажется.
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/shell"
)

// ErrUnsafePath — путь не прошёл проверку безопасности.
var ErrUnsafePath = errors.New("refusing to delete this path")

// protectedInHome — папки внутри домашней, которые нельзя удалять целиком
// ни при каких условиях: там настройки, почта, пароли, iCloud, данные приложений.
var protectedInHome = []string{
	"Library/Application Support",
	"Library/Caches",
	"Library/Containers",
	"Library/Developer",
	"Library/Group Containers",
	"Library/Keychains",
	"Library/Mail",
	"Library/Messages",
	"Library/Mobile Documents",
	"Library/Preferences",
	"Library/Android",
}

// Local выполняет действия на этом компьютере.
type Local struct {
	// Home — домашняя папка. Без пароля удалять можно только внутри неё,
	// минимум на два уровня глубже: ~/.npm/_cacache можно, ~/Projects — нет.
	Home string

	// AdminRoots — где можно удалять с паролем администратора
	// (кэш dyld симуляторов принадлежит root).
	AdminRoots []string

	// runAdmin выполняет строку в оболочке с правами администратора.
	// Поле, а не обычная функция, чтобы тест мог подменить его фейком
	// и не показывать окно ввода пароля.
	runAdmin func(ctx context.Context, shellCmd string) error

	// trash перемещает путь в Корзину. Поле — чтобы тест не отправлял
	// файлы в настоящую Корзину пользователя.
	trash func(ctx context.Context, path string) error
}

// New создаёт исполнитель для текущего пользователя.
func New() (*Local, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	return &Local{
		Home:       home,
		AdminRoots: []string{"/Library/Developer/CoreSimulator/Caches"},
		runAdmin:   runWithAdminPrompt,
		trash:      moveToTrash,
	}, nil
}

// Run выполняет одно действие. Метод с таким именем и такой сигнатурой
// делает *Local реализацией интерфейса core.Executor.
func (e *Local) Run(ctx context.Context, a core.Action) error {
	switch {
	case a.RemovePath != "" && len(a.Command) > 0:
		return errors.New("action has both a path and a command")
	case a.RemovePath != "":
		return e.remove(ctx, a)
	case len(a.Command) > 0:
		return e.command(ctx, a)
	default:
		return errors.New("empty action")
	}
}

// CheckPath — защита от катастрофы. Путь должен быть абсолютным, «чистым»
// (без .. и лишних слешей) и лежать там, где удалять разрешено.
func (e *Local) CheckPath(path string, admin bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%w: %q is not a clean absolute path", ErrUnsafePath, path)
	}

	if admin {
		for _, root := range e.AdminRoots {
			if isInside(path, root) {
				return nil
			}
		}
		return fmt.Errorf("%w: %s is outside %v", ErrUnsafePath, path, e.AdminRoots)
	}

	rel, err := filepath.Rel(e.Home, path) // путь относительно домашней папки
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%w: %s is outside your home folder", ErrUnsafePath, path)
	}

	// Глубина: "Projects" — 1 уровень (нельзя), ".npm/_cacache" — 2 (можно).
	if len(strings.Split(rel, string(filepath.Separator))) < 2 {
		return fmt.Errorf("%w: %s is a top-level folder of your home", ErrUnsafePath, path)
	}

	for _, p := range protectedInHome {
		if rel == p {
			return fmt.Errorf("%w: %s holds data of other apps", ErrUnsafePath, path)
		}
	}

	return nil
}

// isInside — лежит ли path строго внутри root (не равен ему).
func isInside(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// remove удаляет файл или папку.
func (e *Local) remove(ctx context.Context, a core.Action) error {
	if err := e.CheckPath(a.RemovePath, a.NeedsAdmin); err != nil {
		return err
	}

	// Уже нет — значит, цель достигнута. errors.Is понимает и
	// «завёрнутые» ошибки, поэтому сравниваем через него, а не через ==.
	if _, err := os.Lstat(a.RemovePath); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if a.Trash {
		return e.trash(ctx, a.RemovePath)
	}

	if a.NeedsAdmin {
		return e.runAdmin(ctx, "/bin/rm -rf "+singleQuote(a.RemovePath))
	}

	err := os.RemoveAll(a.RemovePath)
	if err != nil && errors.Is(err, fs.ErrPermission) {
		// Папки только для чтения (так устроен кэш модулей Go) RemoveAll
		// удалить не может. Даём себе право записи и пробуем ещё раз.
		makeWritable(a.RemovePath)
		err = os.RemoveAll(a.RemovePath)
	}

	return err
}

// makeWritable добавляет владельцу право записи на все папки внутри path.
func makeWritable(path string) {
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			if info, err := d.Info(); err == nil {
				_ = os.Chmod(p, info.Mode().Perm()|0o700) // 0o700 — rwx для владельца
			}
		}
		return nil
	})
}

// ErrAdminCommand — команды с правами администратора запрещены: с паролем
// разрешено только удалять пути, и только внутри AdminRoots. Иначе ошибка
// в провайдере могла бы выполнить что угодно от имени root.
var ErrAdminCommand = errors.New("commands with administrator rights are not allowed")

// command запускает программу.
func (e *Local) command(ctx context.Context, a core.Action) error {
	if a.NeedsAdmin {
		return ErrAdminCommand
	}

	// Без оболочки: аргументы уходят программе как есть.
	_, err := shell.Run(ctx, a.Command[0], a.Command[1:]...)
	return err
}

// singleQuote берёт строку в одинарные кавычки для оболочки sh.
// Внутри одинарных кавычек не работает ничего ($, `, \), кроме самой
// кавычки. Поэтому каждую кавычку внутри заменяем на четыре символа:
// закрыть кавычки, экранированная кавычка, открыть кавычки снова.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// moveToTrash перемещает путь в Корзину встроенной командой macOS
// /usr/bin/trash (без оболочки, путь передаётся как есть).
func moveToTrash(ctx context.Context, path string) error {
	_, err := shell.Run(ctx, "/usr/bin/trash", path)
	return err
}

// runWithAdminPrompt выполняет команду с правами администратора через
// AppleScript: macOS сама покажет стандартное окно ввода пароля.
// Сам пароль программа не видит.
func runWithAdminPrompt(ctx context.Context, shellCmd string) error {
	// В строке AppleScript нужно экранировать \ и ".
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(shellCmd)
	script := `do shell script "` + escaped + `" with administrator privileges`

	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
