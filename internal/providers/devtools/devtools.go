// Package devtools — провайдер «Инструменты разработки»: редакторы, IDE
// и AI-помощники (Cursor, VS Code, Claude Code, Codex, JetBrains,
// Android Studio и другие — список в tools.go).
//
// Категории — по тому, что лежит, а в имени строки видно, чьё это:
//   - остатки удалённых программ — программы нет, а данные остались;
//   - старые версии — прошлые версии CLI, расширений и IDE;
//   - кэши и временные файлы — пока программа работает, не трогаем;
//   - скачанные среды выполнения (Codex);
//   - только для информации — то, что правильно чистить в самой программе.
package devtools

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/shell"
)

//go:embed translations
var translations embed.FS

const (
	catLeftovers   = "devtools.leftovers"
	catOldVersions = "devtools.old_versions"
	catCaches      = "devtools.caches"
	catRuntimes    = "devtools.runtimes"
	catInfo        = "devtools.info"

	reasonRunning        = "devtools.running"         // программа запущена
	reasonVersionRunning = "devtools.version_running" // запущена именно эта версия
	reasonClaudeApp      = "devtools.claude_app"      // чистить в настройках Claude
	reasonClaudeHistory  = "devtools.claude_history"  // история Claude Code
)

// Provider ищет файлы инструментов разработки.
type Provider struct {
	home      string
	appDirs   []string                       // где искать .app: /Applications и ~/Applications
	hasCLI    func(name string) bool         // установлена ли команда; в тестах — фейк
	processes func(context.Context) []string // список процессов; в тестах — фейк
	tools     []tool
}

// New — провайдер для текущего пользователя.
func New() *Provider {
	home := kit.Home()
	return &Provider{
		home:    home,
		appDirs: []string{"/Applications", filepath.Join(home, "Applications")},
		hasCLI: func(name string) bool {
			_, ok := shell.LookPath(name)
			return ok
		},
		processes: kit.Processes,
		tools:     tools,
	}
}

func (p *Provider) ID() string { return "devtools" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		// Вместе с остатками уходят настройки — на случай, если программу поставят снова.
		{ID: catLeftovers, Risk: core.RiskCaution},
		{ID: catOldVersions, Risk: core.RiskSafe},
		{ID: catCaches, Risk: core.RiskSafe},
		{ID: catRuntimes, Risk: core.RiskCaution},
		{ID: catInfo, Risk: core.RiskCaution},
	}
}

// found — что нашли до подсчёта размеров.
type found struct {
	item core.Item
	path string
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	processes := p.processes(ctx)
	var all []found

	// add — запомнить путь, если он существует.
	add := func(cat, toolName, label, path string, busy *core.Reason) {
		if _, err := os.Lstat(path); err != nil {
			return
		}
		it := core.Item{ID: path, CategoryID: cat, Name: toolName + " · " + label, Detail: kit.Tilde(path)}
		if busy != nil {
			it.Status = core.StatusRunning
			it.Busy = busy
		}
		all = append(all, found{item: it, path: path})
	}

	for _, t := range p.tools {
		installed := p.installed(t)
		var running *core.Reason
		if app := runningName(t, processes); app != "" {
			running = &core.Reason{Key: reasonRunning, Args: map[string]any{"App": app}}
		}

		// Только для информации — всегда, установлена программа или нет.
		for _, inf := range t.info {
			path := p.abs(inf.path)
			if _, err := os.Lstat(path); err == nil {
				all = append(all, found{path: path, item: core.Item{
					ID: path, CategoryID: catInfo, Name: t.name + " · " + label(inf.path), Detail: kit.Tilde(path),
					Protected: &core.Reason{Key: inf.reason},
				}})
			}
		}

		if !installed {
			// Программы нет — всё, что от неё осталось, одной категорией.
			for _, d := range t.data {
				add(catLeftovers, t.name, label(d), p.abs(d), running)
			}
			continue
		}

		// Старые версии CLI.
		if t.versions != nil {
			for _, v := range p.oldVersions(*t.versions) {
				var busy *core.Reason
				if kit.AnyProcess(processes, v) {
					busy = &core.Reason{Key: reasonVersionRunning, Args: map[string]any{"App": t.name}}
				}
				add(catOldVersions, t.name, filepath.Base(v), v, busy)
			}
		}

		// Старые версии расширений: не используются, даже пока редактор открыт.
		if t.extDir != "" {
			for _, dir := range obsoleteExtensions(p.abs(t.extDir)) {
				add(catOldVersions, t.name, filepath.Base(dir), dir, nil)
			}
		}

		// Кэши приложения на Electron и прочие кэши — заняты, пока программа работает.
		if t.support != "" {
			for _, name := range electronCaches {
				add(catCaches, t.name, name, filepath.Join(p.abs(t.support), name), running)
			}
		}
		for _, c := range t.caches {
			add(catCaches, t.name, label(c), p.abs(c), running)
		}

		// Скачанные среды выполнения: каждая подпапка — отдельная среда.
		for _, r := range t.runtimes {
			for _, dir := range kit.Subfolders(p.abs(r)) {
				add(catRuntimes, t.name, filepath.Base(dir), dir, running)
			}
		}
	}

	// JetBrains и Android Studio — папки по версиям.
	for _, f := range p.versionedFolders() {
		all = append(all, f)
	}

	// Размеры — параллельно; пустое не показываем.
	paths := make([]string, len(all))
	for i, f := range all {
		paths[i] = f.path
	}
	sizes := diskutil.SizeAll(ctx, paths, kit.Workers())

	var items []core.Item
	for i, f := range all {
		if sizes[i] == 0 {
			continue
		}
		f.item.Size = sizes[i]
		if f.item.CategoryID == catRuntimes {
			if v := bundleVersion(f.path); v != "" {
				f.item.Detail += " · " + v
			}
		}
		items = append(items, f.item)
	}

	slices.SortStableFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })
	return items, nil
}

// Plan — удалить найденное. Папку прямо в домашней папке (~/.vscode)
// исполнитель удалять целиком не разрешает — это защита от катастрофы.
// Поэтому для неё удаляем всё, что внутри; пустая папка останется.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	var actions []core.Action
	for _, it := range items {
		if filepath.Dir(it.ID) != p.home {
			actions = append(actions, core.Action{RemovePath: it.ID})
			continue
		}
		entries, err := os.ReadDir(it.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			actions = append(actions, core.Action{RemovePath: filepath.Join(it.ID, e.Name())})
		}
	}
	return actions, nil
}

// abs — полный путь от домашней папки.
func (p *Provider) abs(rel string) string {
	return filepath.Join(p.home, rel)
}

// installed — установлена ли программа: есть .app или команда в терминале.
func (p *Provider) installed(t tool) bool {
	for _, app := range t.apps {
		for _, dir := range p.appDirs {
			if _, err := os.Stat(filepath.Join(dir, app+".app")); err == nil {
				return true
			}
		}
	}
	for _, cli := range t.clis {
		if p.hasCLI(cli) {
			return true
		}
	}
	return false
}

// runningName — имя работающей программы ("" — не работает).
func runningName(t tool, processes []string) string {
	for _, app := range t.apps {
		if kit.AnyProcess(processes, "/"+app+".app/") {
			return app
		}
	}
	for _, m := range t.markers {
		if kit.AnyProcess(processes, m) {
			return t.name
		}
	}
	return ""
}

// label — короткое имя пути для строки: без ~/ и Library/.
// "Library/Application Support/Code" → "Application Support/Code".
func label(rel string) string {
	return strings.TrimPrefix(rel, "Library/")
}

// oldVersions — версии CLI, кроме текущей (той, на которую указывает link).
// Если ссылку прочитать не удалось, текущей считается самая новая по дате.
func (p *Provider) oldVersions(v versionsT) []string {
	dir := p.abs(v.dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	current := ""
	if target, err := filepath.EvalSymlinks(p.abs(v.link)); err == nil {
		current = target
	}

	var paths []string
	var newest string
	var newestTime int64
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		paths = append(paths, path)
		if info, err := e.Info(); err == nil && info.ModTime().UnixNano() > newestTime {
			newest, newestTime = path, info.ModTime().UnixNano()
		}
	}
	if current == "" {
		current = newest
	}

	return slices.DeleteFunc(paths, func(path string) bool {
		resolved, _ := filepath.EvalSymlinks(path)
		return path == current || resolved == current || strings.HasPrefix(current, path+"/")
	})
}

// obsoleteExtensions — папки расширений, которые редактор больше не
// использует: их нет в списке установленных (extensions.json).
// Нет списка — не показываем ничего: без него нужное не отличить от старого.
func obsoleteExtensions(extDir string) []string {
	data, err := os.ReadFile(filepath.Join(extDir, "extensions.json"))
	if err != nil {
		return nil
	}
	var installed []struct {
		RelativeLocation string `json:"relativeLocation"`
	}
	if json.Unmarshal(data, &installed) != nil || len(installed) == 0 {
		return nil
	}

	keep := map[string]bool{}
	for _, e := range installed {
		keep[e.RelativeLocation] = true
	}

	var old []string
	for _, dir := range kit.Subfolders(extDir) {
		if name := filepath.Base(dir); !keep[name] && !strings.HasPrefix(name, ".") {
			old = append(old, dir)
		}
	}
	return old
}

// versionRe — "IntelliJIdea2024.2" → "IntelliJIdea", "2024.2".
var versionRe = regexp.MustCompile(`^(.*?)(\d{4}\.\d+)$`)

// versionedFolders — папки JetBrains и Android Studio по версиям.
// Программа установлена — показываем все версии, кроме самой новой
// (после обновления IDE переносит настройки в новую папку, старая не нужна).
// Не установлена — показываем все как остатки.
func (p *Provider) versionedFolders() []found {
	var out []found

	for _, group := range versioned {
		for _, prod := range group.products {
			installed := p.installed(tool{apps: prod.apps})

			type dirVersion struct {
				path, version string
			}
			var dirs []dirVersion
			newest := ""
			for _, base := range group.dirs {
				for _, dir := range kit.Subfolders(p.abs(base)) {
					m := versionRe.FindStringSubmatch(filepath.Base(dir))
					if m == nil || m[1] != prod.prefix {
						continue
					}
					dirs = append(dirs, dirVersion{dir, m[2]})
					if compareVersions(m[2], newest) > 0 {
						newest = m[2]
					}
				}
			}

			for _, d := range dirs {
				cat := catLeftovers
				if installed {
					if d.version == newest {
						continue // текущая версия — нужна
					}
					cat = catOldVersions
				}
				rel, _ := filepath.Rel(filepath.Join(p.home, "Library"), d.path)
				out = append(out, found{path: d.path, item: core.Item{
					ID: d.path, CategoryID: cat, Name: prod.name + " · " + rel, Detail: kit.Tilde(d.path),
				}})
			}
		}
	}
	return out
}

// compareVersions сравнивает "2024.2" и "2025.1" как числа.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(as), len(bs)) {
		var x, y int
		if i < len(as) {
			x = atoi(as[i])
		}
		if i < len(bs) {
			y = atoi(bs[i])
		}
		if c := cmp.Compare(x, y); c != 0 {
			return c
		}
	}
	return 0
}

// atoi — число из строки цифр (не цифры — 0).
func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// bundleVersion — версия сборки из runtime.json среды Codex ("" — нет).
func bundleVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "runtime.json"))
	if err != nil {
		return ""
	}
	var r struct {
		BundleVersion string `json:"bundleVersion"`
	}
	if json.Unmarshal(data, &r) != nil {
		return ""
	}
	return r.BundleVersion
}
