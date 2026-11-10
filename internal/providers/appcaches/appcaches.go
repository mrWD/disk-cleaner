// Package appcaches is the provider for app caches (~/Library/Caches),
// leftovers of app updates and logs (~/Library/Logs).
//
// A cache is what an app can recreate: images, downloaded
// pages, compiled shaders. While the app is closed, its cache
// can be deleted without loss. While it runs, it cannot: the app keeps the files
// open and may break.
//
// We do not touch:
//   - folders that already have an owner (package caches, see kit.LibraryCachesOwned);
//   - Apple system caches (com.apple.*): macOS manages them itself;
//   - Claude and Claude Code caches and logs: they are cleaned in the settings of Claude itself.
package appcaches

import (
	"cmp"
	"context"
	"embed"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
	"disk-cleaner/internal/providers/kit"
)

//go:embed translations
var translations embed.FS

const (
	catCaches  = "appcaches.caches"
	catUpdates = "appcaches.updates"
	catLogs    = "appcaches.logs"

	// Reason: the app this cache belongs to is running now.
	reasonRunning = "appcaches.running"
)

// Provider looks for caches and logs in the home folder.
type Provider struct {
	home      string
	processes func(ctx context.Context) []string

	// We do not show anything smaller than these sizes, to keep small stuff out of the list.
	minCache core.Bytes
	minLog   core.Bytes
}

// New is the provider for the current user.
func New() *Provider {
	return &Provider{home: kit.Home(), processes: kit.Processes, minCache: 10_000_000, minLog: 1_000_000}
}

func (p *Provider) ID() string { return "appcaches" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catCaches, Risk: core.RiskSafe},
		{ID: catUpdates, Risk: core.RiskSafe},
		{ID: catLogs, Risk: core.RiskSafe},
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	running := runningApps(p.processes(ctx))

	// --- ~/Library/Caches: app caches and update leftovers ---
	var cachePaths []string
	for _, dir := range kit.Subfolders(filepath.Join(p.home, "Library", "Caches")) {
		name := filepath.Base(dir)
		if kit.LibraryCachesOwned[name] || skip(name) {
			continue
		}
		cachePaths = append(cachePaths, dir)
	}

	var items []core.Item
	sizes := diskutil.SizeAll(ctx, cachePaths, kit.Workers())
	for i, dir := range cachePaths {
		if sizes[i] < p.minCache {
			continue
		}
		name := filepath.Base(dir)
		it := core.Item{ID: dir, CategoryID: catCaches, Name: name, Detail: kit.Tilde(dir), Size: sizes[i]}
		if isUpdaterLeftover(name) {
			it.CategoryID = catUpdates
		}
		if app := owner(name, running); app != "" {
			it.Status = core.StatusRunning
			it.Busy = &core.Reason{Key: reasonRunning, Args: map[string]any{"App": app}}
		}
		items = append(items, it)
	}

	// --- ~/Library/Logs: log folders and files ---
	logsDir := filepath.Join(p.home, "Library", "Logs")
	entries, _ := os.ReadDir(logsDir)
	var logPaths []string
	for _, e := range entries {
		if !skip(e.Name()) && !kit.LibraryLogsOwned[e.Name()] {
			logPaths = append(logPaths, filepath.Join(logsDir, e.Name()))
		}
	}
	logSizes := diskutil.SizeAll(ctx, logPaths, kit.Workers())
	for i, path := range logPaths {
		if logSizes[i] >= p.minLog {
			items = append(items, core.Item{
				ID: path, CategoryID: catLogs, Name: filepath.Base(path), Detail: kit.Tilde(path), Size: logSizes[i],
			})
		}
	}

	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })
	return items, nil
}

func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	return kit.RemoveActions(items), nil
}

// appleSystemCaches are Apple system caches without the com.apple. prefix.
var appleSystemCaches = map[string]bool{
	"GeoServices": true, "CloudKit": true, "Metadata": true, "PassKit": true,
	"FamilyCircle": true, "familycircled": true, "SiriTTS": true,
}

// skip are folders this section does not show: hidden ones, Apple system
// caches and everything that belongs to Claude and Claude Code.
func skip(name string) bool {
	return strings.HasPrefix(name, ".") ||
		strings.HasPrefix(name, "com.apple.") ||
		appleSystemCaches[name] ||
		strings.Contains(strings.ToLower(name), "claude") ||
		strings.HasPrefix(name, "com.anthropic.")
}

// isUpdaterLeftover is a folder left by the update mechanism
// of an app: ShipIt (Electron apps: Cursor, VS Code) or *-updater.
func isUpdaterLeftover(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "shipit") || strings.HasSuffix(n, "updater")
}

// appRe finds the app name in the command line of a process:
// "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" → "Google Chrome".
var appRe = regexp.MustCompile(`/([^/]+)\.app/Contents/`)

// runningApps are the names of the running apps.
func runningApps(processes []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range processes {
		for _, m := range appRe.FindAllStringSubmatch(p, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	return out
}

// genericWords are parts of folder names that do not let you guess the app.
var genericWords = map[string]bool{
	"com": true, "org": true, "net": true, "app": true, "apps": true, "mac": true,
	"cache": true, "caches": true, "helper": true, "shipit": true, "updater": true,
}

// owner is which of the running apps seems to own a cache with this
// name ("" means none). We compare words from the folder name with the
// app names: "Google" ↔ "Google Chrome", "com.spotify.client" ↔ "Spotify".
// If we make a mistake, it is in the safe direction: we block too much.
func owner(cacheName string, apps []string) string {
	words := strings.FieldsFunc(strings.ToLower(cacheName), func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == ' '
	})

	for _, app := range apps {
		normalized := strings.ToLower(strings.ReplaceAll(app, " ", ""))
		for _, w := range words {
			if len(w) >= 4 && !genericWords[w] && strings.Contains(normalized, w) {
				return app
			}
		}
	}
	return ""
}
