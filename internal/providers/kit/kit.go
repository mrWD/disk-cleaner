// Package kit holds shared building blocks for providers that look for folders:
// "folder -> item", "subfolders -> items", "items -> delete actions".
//
// Providers don't import each other, but they may use kit:
// it is a helper just like diskutil or shell.
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

// LibraryCachesOwned lists folders inside ~/Library/Caches that already have
// an owner provider ("Package caches" or "Developer tools").
// The "App caches & logs" provider skips them: every path must have
// exactly one owner, otherwise its size is counted twice and two sections
// would try to delete the same thing. The tests in packagecaches and devtools
// check that this list keeps up with them.
var LibraryCachesOwned = map[string]bool{
	"Yarn": true, "CocoaPods": true, "pip": true, "Homebrew": true, "go-build": true,
	"org.swift.swiftpm": true, "ms-playwright": true, "ms-playwright-mcp": true,
	"electron": true, "electron-builder": true, "ReactNative": true, "typescript": true,
	"pnpm": true, "node-gyp": true, "dotslash": true, "com.apple.python": true,

	// Developer tools.
	"claude-cli-nodejs": true, "com.openai.codex": true, "com.todesktop.230313mzl4w4u92": true,
	"com.microsoft.VSCode": true, "dev.zed.Zed": true, "VisualStudio": true, "JetBrains": true,
}

// LibraryLogsOwned is the same for ~/Library/Logs: logs shown by the
// "Developer tools" section.
var LibraryLogsOwned = map[string]bool{"JetBrains": true, "VisualStudio": true}

// Home is the current user's home folder ("" if it could not be found out).
func Home() string {
	home, _ := os.UserHomeDir() // _ means we deliberately ignore the error
	return home
}

// Tilde replaces the home folder with ~ so paths in the UI are shorter:
// /Users/viktor/.npm → ~/.npm.
func Tilde(path string) string {
	home := Home()
	if home != "" && strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// Workers is how many folders to measure at once: one per CPU core, but at most 8.
func Workers() int {
	return min(runtime.NumCPU(), 8) // min is a built-in function since Go 1.21
}

// FolderItem is one item made from one folder. The second value is false
// if the folder is missing or empty: there is nothing to show.
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

// Subfolders returns the full paths of parent's subfolders in alphabetical order. No folder means an empty slice.
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

// PathItems makes one item per path; sizes are measured in parallel.
// name turns a path into a display name. Empty folders are skipped.
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

	// Biggest on top: that is what the user came for.
	// cmp.Compare returns -1, 0 or 1; b and a are swapped, so the sort is descending.
	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })
	return items
}

// BaseName is the display name: the last part of the path.
func BaseName(path string) string {
	return filepath.Base(path)
}

// RemoveActions makes one "delete this path" action per item (the ID is the path).
func RemoveActions(items []core.Item) []core.Action {
	actions := make([]core.Action, 0, len(items))
	for _, it := range items {
		actions = append(actions, core.Action{RemovePath: it.ID})
	}
	return actions
}

// Processes returns the command lines of all running processes. It is needed to
// find out what is running right now: an Android emulator, a Gradle daemon, a dev server.
func Processes(ctx context.Context) []string {
	// ps -axo command= means all processes, command line only, no header.
	out, err := shell.Run(ctx, "/bin/ps", "-axo", "command=")
	if err != nil {
		return nil
	}
	return strings.Split(string(out), "\n")
}

// AnyProcess reports whether any process has substr in its command line.
func AnyProcess(processes []string, substr string) bool {
	return FindProcess(processes, substr) != ""
}

// FindProcess returns the program name of the first process whose command line
// contains substr, or "" if there is none:
// "/usr/bin/java -cp … GradleDaemon" → "java".
func FindProcess(processes []string, substr string) string {
	for _, p := range processes {
		if !strings.Contains(p, substr) {
			continue
		}
		fields := strings.Fields(p) // split on whitespace
		if len(fields) == 0 {
			return "?"
		}
		return filepath.Base(fields[0])
	}
	return ""
}
