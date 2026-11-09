// Package shell — запуск внешних программ (docker, xcrun, git, ps).
//
// Несмотря на имя, командная оболочка (zsh, bash) здесь не участвует:
// программа запускается напрямую, аргументы передаются списком.
// Поэтому пробелы, кавычки и ; в аргументах не могут «сломать» команду.
package shell

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// extraDirs — где искать программы, кроме PATH.
//
// Приложение, запущенное из Finder (а не из терминала), получает урезанный
// PATH: /usr/bin:/bin:/usr/sbin:/sbin. Без этих папок оно не нашло бы
// brew, docker, go и помощники docker для входа в реестры.
func extraDirs() []string {
	dirs := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/go/bin",
		"/Applications/Docker.app/Contents/Resources/bin",
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
		)
	}

	return dirs
}

// searchPath — PATH процесса плюс extraDirs, без повторов.
func searchPath() []string {
	seen := map[string]bool{}
	var out []string

	// filepath.SplitList разбивает строку PATH по двоеточиям.
	for _, dir := range append(filepath.SplitList(os.Getenv("PATH")), extraDirs()...) {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}

	return out
}

// LookPath находит программу по имени и возвращает полный путь к ней.
// Второе значение — нашлась ли программа.
func LookPath(name string) (string, bool) {
	if filepath.IsAbs(name) {
		info, err := os.Stat(name)
		return name, err == nil && !info.IsDir()
	}

	for _, dir := range searchPath() {
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		// Mode()&0o111 — есть ли у файла право на запуск (бит x).
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return full, true
		}
	}

	return "", false
}

// Env — окружение для запускаемых программ: текущее, но с полным PATH.
// docker, например, сам ищет в PATH своего помощника docker-credential-desktop.
func Env() []string {
	env := os.Environ()
	path := "PATH=" + strings.Join(searchPath(), string(os.PathListSeparator))

	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = path
			return env
		}
	}

	return append(env, path)
}

// Runner — функция запуска программы. Провайдеры получают её снаружи,
// чтобы в тестах подставить фейк с заранее записанным выводом
// и не зависеть от того, установлен ли на машине docker.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Run запускает программу и возвращает её стандартный вывод.
// Если программа завершилась с ошибкой, ошибка содержит её stderr:
// иначе было бы непонятно, почему docker «упал».
func Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, ok := LookPath(name)
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, exec.ErrNotFound)
	}

	// CommandContext убьёт процесс, если ctx отменят или истечёт его таймаут.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = Env()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		// %w «заворачивает» исходную ошибку: errors.Is/As смогут до неё добраться.
		return stdout.Bytes(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
	}

	return stdout.Bytes(), nil
}
