// Package kit — общие кирпичики для провайдеров, которые ищут папки:
// «папка → объект», «подпапки → объекты», «объекты → действия удаления».
//
// Провайдеры не импортируют друг друга, но могут пользоваться kit:
// это такой же хелпер, как diskutil или shell.
package kit

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
	"disk-cleaner/internal/shell"
)

// LibraryCachesOwned — папки внутри ~/Library/Caches, у которых уже есть
// провайдер-владелец («Кэши пакетов» или «Инструменты разработки»).
// Провайдер «Кэши приложений» их пропускает: у каждого пути должен быть
// ровно один владелец, иначе размер посчитается дважды, а удалить одно
// и то же попытаются два раздела. Тесты в packagecaches и devtools
// проверяют, что этот список не отстаёт от них.
var LibraryCachesOwned = map[string]bool{
	"Yarn": true, "CocoaPods": true, "pip": true, "Homebrew": true, "go-build": true,
	"org.swift.swiftpm": true, "ms-playwright": true, "ms-playwright-mcp": true,
	"electron": true, "electron-builder": true, "ReactNative": true, "typescript": true,
	"pnpm": true, "node-gyp": true, "dotslash": true, "com.apple.python": true,

	// Инструменты разработки.
	"claude-cli-nodejs": true, "com.openai.codex": true, "com.todesktop.230313mzl4w4u92": true,
	"com.microsoft.VSCode": true, "dev.zed.Zed": true, "VisualStudio": true, "JetBrains": true,
}

// LibraryLogsOwned — то же для ~/Library/Logs: логи, которые показывает
// раздел «Инструменты разработки».
var LibraryLogsOwned = map[string]bool{"JetBrains": true, "VisualStudio": true}

// Home — домашняя папка текущего пользователя ("" если не удалось узнать).
func Home() string {
	home, _ := os.UserHomeDir() // _ — ошибку сознательно игнорируем
	return home
}

// Tilde заменяет домашнюю папку на ~, чтобы пути в интерфейсе были короче:
// /Users/viktor/.npm → ~/.npm.
func Tilde(path string) string {
	home := Home()
	if home != "" && strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// Workers — сколько папок считать одновременно: по числу ядер, но не больше 8.
func Workers() int {
	return min(runtime.NumCPU(), 8) // min — встроенная функция Go 1.21+
}

// FolderItem — один объект из одной папки. Второе значение false,
// если папки нет или она пустая: показывать там нечего.
func FolderItem(ctx context.Context, categoryID, name, path string) (core.Item, bool) {
	size, err := diskutil.DirSize(ctx, path)
	if err != nil || size == 0 {
		return core.Item{}, false
	}

	return core.Item{
		ID:         path,
		CategoryID: categoryID,
		Name:       name,
		Detail:     Tilde(path),
		Size:       size,
	}, true
}

// Subfolders — полные пути подпапок parent по алфавиту. Нет папки — пустой срез.
func Subfolders(parent string) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}

	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(parent, e.Name()))
		}
	}
	return out
}

// PathItems — по объекту на каждый путь, размеры считаются параллельно.
// name делает из пути имя для показа. Пустые папки пропускаются.
func PathItems(ctx context.Context, categoryID string, paths []string, name func(path string) string) []core.Item {
	sizes := diskutil.SizeAll(ctx, paths, Workers())

	var items []core.Item
	for i, p := range paths {
		if sizes[i] == 0 {
			continue
		}
		items = append(items, core.Item{
			ID:         p,
			CategoryID: categoryID,
			Name:       name(p),
			Detail:     Tilde(p),
			Size:       sizes[i],
		})
	}

	// Самое большое — сверху: ради него пользователь и пришёл.
	// cmp.Compare возвращает -1, 0 или 1; b и a переставлены — сортировка по убыванию.
	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })
	return items
}

// BaseName — имя для показа: последняя часть пути.
func BaseName(path string) string {
	return filepath.Base(path)
}

// RemoveActions — по действию «удалить путь» на каждый объект (ID — это путь).
func RemoveActions(items []core.Item) []core.Action {
	actions := make([]core.Action, 0, len(items))
	for _, it := range items {
		actions = append(actions, core.Action{RemovePath: it.ID})
	}
	return actions
}

// Processes — командные строки всех запущенных процессов. Нужно, чтобы
// узнать, что сейчас запущено: эмулятор Android, демон Gradle, dev-сервер.
func Processes(ctx context.Context) []string {
	// ps -axo command= — все процессы, только командная строка, без заголовка.
	out, err := shell.Run(ctx, "/bin/ps", "-axo", "command=")
	if err != nil {
		return nil
	}
	return strings.Split(string(out), "\n")
}

// AnyProcess — есть ли процесс, в командной строке которого есть substr.
func AnyProcess(processes []string, substr string) bool {
	return FindProcess(processes, substr) != ""
}

// FindProcess — имя программы первого процесса, в командной строке
// которого есть substr, или "" если такого нет:
// "/usr/bin/java -cp … GradleDaemon" → "java".
func FindProcess(processes []string, substr string) string {
	for _, p := range processes {
		if !strings.Contains(p, substr) {
			continue
		}
		fields := strings.Fields(p) // разбить по пробелам
		if len(fields) == 0 {
			return "?"
		}
		return filepath.Base(fields[0])
	}
	return ""
}
