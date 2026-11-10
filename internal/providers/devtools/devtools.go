// Package devtools is the "Developer tools" provider: editors, IDEs
// and AI assistants (Cursor, VS Code, Claude Code, Codex, JetBrains,
// Android Studio and others; the list is in tools.go).
//
// Categories go by what is lying there, and the row name shows whose it is:
//   - leftovers of uninstalled apps: the app is gone but the data stayed;
//   - old versions: earlier versions of CLIs, extensions and IDEs;
//   - caches and temporary files: not touched while the app runs;
//   - downloaded runtimes (Codex);
//   - for information only: what is right to clean inside the app itself,
//     including Claude Code's chat history, project by project.
//
// Every row also says when its tool was last used (core.Item.LastUsed):
// for an app, the date macOS keeps of when it was last opened; for a CLI,
// its newest saved session; for leftovers of an uninstalled app, the newest
// file among them. Something untouched for months is easier to let go.
package devtools

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

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

	reasonRunning        = "devtools.running"         // the app is running
	reasonVersionRunning = "devtools.version_running" // exactly this version is running
	reasonClaudeApp      = "devtools.claude_app"      // clean it in the Claude settings
	reasonClaudeHistory  = "devtools.claude_history"  // Claude Code history
)

// Provider looks for development tool files.
type Provider struct {
	home      string
	appDirs   []string                       // where to look for .app: /Applications and ~/Applications
	hasCLI    func(name string) bool         // whether the command is installed; a fake in tests
	processes func(context.Context) []string // list of processes; a fake in tests
	tools     []tool

	appLastUsed func(app string) time.Time // when an .app was last opened; a fake in tests
	historyMin  core.Bytes                 // a project's history gets its own row from this size
}

// New is the provider for the current user.
func New() *Provider {
	home := kit.Home()
	return &Provider{
		home:    home,
		appDirs: []string{"/Applications", filepath.Join(home, "Applications")},
		hasCLI: func(name string) bool {
			_, ok := shell.LookPath(name)
			return ok
		},
		processes:   kit.Processes,
		tools:       tools,
		appLastUsed: spotlightLastUsed,
		historyMin:  10_000_000,
	}
}

func (p *Provider) ID() string { return "devtools" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		// Caution: the app's settings go away together with the leftovers, and
		// they'd be missed if the app is installed again.
		{ID: catLeftovers, Risk: core.RiskCaution},
		{ID: catOldVersions, Risk: core.RiskSafe},
		{ID: catCaches, Risk: core.RiskSafe},
		{ID: catRuntimes, Risk: core.RiskCaution},
		{ID: catInfo, Risk: core.RiskCaution},
	}
}

// found is what we found before counting sizes.
type found struct {
	item core.Item
	path string
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	processes := p.processes(ctx)
	var all []found
	var history []core.Item // Claude Code's history: sized separately, see history()

	// used is when the tool being looked at was last used; add puts it on its rows.
	var used time.Time

	// add remembers a path if it exists.
	add := func(cat, toolName, label, path string, busy *core.Reason) {
		if _, err := os.Lstat(path); err != nil {
			return
		}
		it := core.Item{ID: path, CategoryID: cat, Name: toolName + " · " + label, Detail: kit.Tilde(path), LastUsed: used}
		if busy != nil {
			it.Status = core.StatusRunning
			it.Busy = busy
		}
		all = append(all, found{item: it, path: path})
	}

	for _, t := range p.tools {
		installed := p.installed(t)
		used = p.lastUsed(ctx, t)
		var running *core.Reason
		if app := runningName(t, processes); app != "" {
			running = &core.Reason{Key: reasonRunning, Args: map[string]any{"App": app}}
		}

		// For information only: always, whether or not the app is installed.
		for _, inf := range t.info {
			path := p.abs(inf.path)
			if _, err := os.Lstat(path); err == nil {
				all = append(all, found{path: path, item: core.Item{
					ID: path, CategoryID: catInfo, Name: t.name + " · " + label(inf.path), Detail: kit.Tilde(path),
					LastUsed: used, Protected: &core.Reason{Key: inf.reason},
				}})
			}
		}
		if t.history != "" {
			history = append(history, p.history(ctx, t)...)
		}

		if !installed {
			// The app is gone: everything left of it goes into one category.
			for _, d := range t.data {
				add(catLeftovers, t.name, label(d), p.abs(d), running)
			}
			continue
		}

		// Old CLI versions.
		if t.versions != nil {
			for _, v := range p.oldVersions(*t.versions) {
				var busy *core.Reason
				if kit.AnyProcess(processes, v) {
					busy = &core.Reason{Key: reasonVersionRunning, Args: map[string]any{"App": t.name}}
				}
				add(catOldVersions, t.name, filepath.Base(v), v, busy)
			}
		}

		// Old extension versions: unused even while the editor is open.
		if t.extDir != "" {
			for _, dir := range obsoleteExtensions(p.abs(t.extDir)) {
				add(catOldVersions, t.name, filepath.Base(dir), dir, nil)
			}
		}

		// Electron app caches and other caches: busy while the app runs.
		if t.support != "" {
			for _, name := range electronCaches {
				add(catCaches, t.name, name, filepath.Join(p.abs(t.support), name), running)
			}
		}
		for _, c := range t.caches {
			add(catCaches, t.name, label(c), p.abs(c), running)
		}

		// Downloaded runtimes: each subfolder is a separate runtime.
		for _, r := range t.runtimes {
			for _, dir := range kit.Subfolders(p.abs(r)) {
				add(catRuntimes, t.name, filepath.Base(dir), dir, running)
			}
		}
	}

	// JetBrains and Android Studio: folders by version.
	for _, f := range p.versionedFolders() {
		all = append(all, f)
	}

	// Sizes in parallel; empty ones are not shown.
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
		if f.item.CategoryID == catLeftovers && f.item.LastUsed.IsZero() {
			// The app is gone, so macOS has no date for it: its newest leftover
			// file says when it was last used, near enough.
			f.item.LastUsed = diskutil.NewestFile(ctx, f.path)
		}
		if f.item.CategoryID == catRuntimes {
			if v := bundleVersion(f.path); v != "" {
				f.item.Detail += " · " + v
			}
		}
		items = append(items, f.item)
	}
	items = append(items, history...)

	slices.SortStableFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })
	return items, nil
}

// Plan deletes what was found. A folder directly in the home folder (~/.vscode)
// is not allowed to be deleted whole by the executor: that protects against a disaster.
// So for it we delete everything inside; the empty folder stays.
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

// abs is the full path from the home folder.
func (p *Provider) abs(rel string) string {
	return filepath.Join(p.home, rel)
}

// installed tells whether the program is installed: there is an .app or a command in the terminal.
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

// runningName is the name of the running program ("" means it is not running).
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

// label is a short name of a path for a row: without ~/ and Library/.
// "Library/Application Support/Code" → "Application Support/Code".
func label(rel string) string {
	return strings.TrimPrefix(rel, "Library/")
}

// oldVersions are the CLI versions other than the current one (the one link points to).
// If the link could not be read, the newest one by date counts as current.
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

// obsoleteExtensions are extension folders the editor no longer
// uses: they are not in the list of installed ones (extensions.json).
// No list means we show nothing: without it, needed ones cannot be told from old ones.
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

// versionedFolders are the JetBrains and Android Studio folders by version.
// If the app is installed, we show all versions except the newest
// (after an update the IDE moves its settings to the new folder, the old one is not needed).
// If it is not installed, we show all of them as leftovers.
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
						continue // current version: it is needed
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

// compareVersions compares "2024.2" and "2025.1" as numbers.
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

// atoi is a number from a string of digits (anything else gives 0).
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

// bundleVersion is the build version from runtime.json of a Codex runtime ("" means none).
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

// lastUsed — when a tool was last used: the latest of when macOS says its
// app was last opened and its newest saved session. Zero if neither is known.
func (p *Provider) lastUsed(ctx context.Context, t tool) time.Time {
	var last time.Time
	later := func(when time.Time) {
		if when.After(last) {
			last = when
		}
	}
	for _, app := range t.apps {
		for _, dir := range p.appDirs {
			if path := filepath.Join(dir, app+".app"); exists(path) && p.appLastUsed != nil {
				later(p.appLastUsed(path))
			}
		}
	}
	for _, s := range t.sessions {
		later(diskutil.NewestFile(ctx, p.abs(s)))
	}
	return last
}

// exists — whether a path exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// spotlightLastUsed asks macOS when an app was last opened. Finder shows the
// same date as "Last opened"; Spotlight keeps it as kMDItemLastUsedDate.
func spotlightLastUsed(app string) time.Time {
	out, err := exec.Command("/usr/bin/mdls", "-raw", "-name", "kMDItemLastUsedDate", app).Output()
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(string(out))) // "(null)" fails: zero
	if err != nil {
		return time.Time{}
	}
	return t
}

// history — Claude Code's chat history, project by project: one row for each
// project whose history is at least historyMin, named after the project's
// folder and dated by its newest session, and one row for all the smaller
// ones together. For information only: it's the user's own conversations,
// and Claude Code cleans old ones itself (cleanupPeriodDays in its settings).
func (p *Provider) history(ctx context.Context, t tool) []core.Item {
	root := p.abs(t.history)
	dirs := kit.Subfolders(root)
	sizes := diskutil.SizeAll(ctx, dirs, kit.Workers())
	protected := &core.Reason{Key: reasonClaudeHistory}

	var items []core.Item
	var rest core.Item // the small ones together
	restCount := 0
	for i, dir := range dirs {
		if sizes[i] == 0 {
			continue
		}
		last := diskutil.NewestFile(ctx, dir)
		if sizes[i] < p.historyMin {
			restCount++
			rest.Size += sizes[i]
			if last.After(rest.LastUsed) {
				rest.LastUsed = last
			}
			continue
		}
		items = append(items, core.Item{
			ID: dir, CategoryID: catInfo, Name: t.name + " · history · " + projectName(dir),
			Detail: kit.Tilde(dir), Size: sizes[i], LastUsed: last, Protected: protected,
		})
	}

	if restCount > 0 {
		noun := "projects"
		if restCount == 1 {
			noun = "project"
		}
		rest.ID = root + "#smaller" // an ID of its own: root itself isn't this row
		rest.CategoryID = catInfo
		rest.Name = fmt.Sprintf("%s · history · %d more %s", t.name, restCount, noun)
		rest.Detail = kit.Tilde(root)
		rest.Protected = protected
		items = append(items, rest)
	}
	return items
}

// cwdRe finds the folders Claude Code records in its messages:
// "cwd":"/Users/me/Projects/disk-cleaner".
var cwdRe = regexp.MustCompile(`"cwd":"((?:[^"\\]|\\.)*)"`)

// encodeRe — the characters Claude Code replaces with "-" when it names a
// history folder after a project folder: "/Users/me/Projects/a.b" → "-Users-me-Projects-a-b".
var encodeRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

// projectName — the folder a history belongs to, like "~/Projects/disk-cleaner".
//
// The history folder's own name can't be turned back reliably ("-" may have
// been "/", "." or a real "-"), so it's checked against the folders the
// sessions mention: the one that gives exactly this name is the project.
// Matching matters: a session that started in a project can move into one of
// its worktrees, so the first folder mentioned isn't always the project's.
// A worktree of a project reads as "~/Projects/app › feature-x".
func projectName(dir string) string {
	want := filepath.Base(dir)
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	slices.SortFunc(files, func(a, b string) int { return cmp.Compare(modTime(b), modTime(a)) }) // newest first
	if len(files) > 5 {
		files = files[:5] // a few sessions are enough to find it
	}

	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		head := make([]byte, 256<<10) // the first lines are enough; a session can be huge
		n, _ := io.ReadFull(f, head)
		f.Close()
		for _, m := range cwdRe.FindAllSubmatch(head[:n], -1) {
			cwd, err := strconv.Unquote(`"` + string(m[1]) + `"`)
			if err == nil && encodeRe.ReplaceAllString(cwd, "-") == want {
				return strings.Replace(kit.Tilde(cwd), "/.claude/worktrees/", " › ", 1)
			}
		}
	}
	return want
}

// modTime — when a file was last changed; zero if it can't be read.
func modTime(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime().UnixNano()
	}
	return 0
}
