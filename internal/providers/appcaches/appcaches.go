// Package appcaches — провайдер кэшей приложений (~/Library/Caches),
// остатков обновлений приложений и логов (~/Library/Logs).
//
// Кэш — это то, что приложение может пересоздать: картинки, скачанные
// страницы, скомпилированные шейдеры. Пока приложение закрыто, его кэш
// можно удалить без потерь. Пока запущено — нельзя: оно держит файлы
// открытыми и может сломаться.
//
// Не трогаем:
//   - папки, у которых уже есть владелец (кэши пакетов, см. kit.LibraryCachesOwned);
//   - системные кэши Apple (com.apple.*) — macOS управляет ими сама;
//   - кэши и логи Claude и Claude Code — их чистят в настройках самого Claude.
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

	// Причина: приложение, чей это кэш, сейчас запущено.
	reasonRunning = "appcaches.running"
)

// Provider ищет кэши и логи в домашней папке.
type Provider struct {
	home      string
	processes func(ctx context.Context) []string

	// Меньше этих размеров не показываем, чтобы не засорять список мелочью.
	minCache core.Bytes
	minLog   core.Bytes
}

// New — провайдер для текущего пользователя.
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

	// --- ~/Library/Caches: кэши приложений и остатки обновлений ---
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

	// --- ~/Library/Logs: папки и файлы логов ---
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

// appleSystemCaches — системные кэши Apple без префикса com.apple.
var appleSystemCaches = map[string]bool{
	"GeoServices": true, "CloudKit": true, "Metadata": true, "PassKit": true,
	"FamilyCircle": true, "familycircled": true, "SiriTTS": true,
}

// skip — папки, которые этот раздел не показывает: скрытые, системные
// кэши Apple и всё, что относится к Claude и Claude Code.
func skip(name string) bool {
	return strings.HasPrefix(name, ".") ||
		strings.HasPrefix(name, "com.apple.") ||
		appleSystemCaches[name] ||
		strings.Contains(strings.ToLower(name), "claude") ||
		strings.HasPrefix(name, "com.anthropic.")
}

// isUpdaterLeftover — папка, которую оставил механизм обновления
// приложения: ShipIt (Electron-приложения: Cursor, VS Code) или *-updater.
func isUpdaterLeftover(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "shipit") || strings.HasSuffix(n, "updater")
}

// appRe находит в командной строке процесса имя приложения:
// "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" → "Google Chrome".
var appRe = regexp.MustCompile(`/([^/]+)\.app/Contents/`)

// runningApps — имена запущенных приложений.
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

// genericWords — части имён папок, по которым приложение не угадать.
var genericWords = map[string]bool{
	"com": true, "org": true, "net": true, "app": true, "apps": true, "mac": true,
	"cache": true, "caches": true, "helper": true, "shipit": true, "updater": true,
}

// owner — какое из запущенных приложений, похоже, владеет кэшем с таким
// именем ("" — ни одно). Сравниваем слова из имени папки с именами
// приложений: "Google" ↔ "Google Chrome", "com.spotify.client" ↔ "Spotify".
// Если ошибёмся, то в безопасную сторону: заблокируем лишнее.
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
