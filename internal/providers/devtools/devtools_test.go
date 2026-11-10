package devtools

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/providers/providertest"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// machine is a home folder that looks like this machine on Nov 9, 2026:
// Cursor, Claude, Claude Code and ChatGPT (Codex) are installed, while VS Code
// and IntelliJ IDEA are removed but left their data.
type machine struct {
	home      string
	apps      string   // the Applications folder
	clis      []string // installed commands
	processes []string
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	m := &machine{home: t.TempDir(), clis: []string{"claude"}}
	m.apps = filepath.Join(m.home, "Applications")
	for _, app := range []string{"Cursor", "Claude", "ChatGPT"} {
		write(t, filepath.Join(m.apps, app+".app", "Contents", "Info.plist"), "")
	}
	h := m.home

	// Cursor: one extension is installed, its old version is not.
	write(t, filepath.Join(h, ".cursor/extensions/extensions.json"), `[{"relativeLocation": "eamodio.gitlens-19.2.0"}]`)
	write(t, filepath.Join(h, ".cursor/extensions/eamodio.gitlens-19.2.0/package.json"), "{}")
	write(t, filepath.Join(h, ".cursor/extensions/eamodio.gitlens-19.1.0/package.json"), "{}")
	write(t, filepath.Join(h, "Library/Application Support/Cursor/CachedData/x.js"), "x")
	write(t, filepath.Join(h, "Library/Application Support/Cursor/User/settings.json"), "{}") // settings: do not touch

	// Claude and Claude Code.
	write(t, filepath.Join(h, "Library/Application Support/Claude/config.json"), "{}")
	write(t, filepath.Join(h, ".claude/projects/p/session.jsonl"), "{}")
	write(t, filepath.Join(h, ".local/share/claude/versions/2.1.220"), "old")
	write(t, filepath.Join(h, ".local/share/claude/versions/2.1.251"), "old")
	write(t, filepath.Join(h, ".local/share/claude/versions/2.1.296"), "current")
	if err := os.MkdirAll(filepath.Join(h, ".local/bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h, ".local/share/claude/versions/2.1.296"), filepath.Join(h, ".local/bin/claude")); err != nil {
		t.Fatal(err)
	}

	// Codex.
	write(t, filepath.Join(h, ".cache/codex-runtimes/codex-primary-runtime/runtime.json"), `{"bundleVersion": "26.1024.11018"}`)
	write(t, filepath.Join(h, ".codex/.tmp/git-1QTtGW/README.md"), "x")
	write(t, filepath.Join(h, ".codex/sessions/chat.jsonl"), "{}") // history is not junk

	// Leftovers of the removed VS Code and IntelliJ IDEA.
	write(t, filepath.Join(h, ".vscode/extensions/ms-python.python-2024.1.0/package.json"), "{}")
	write(t, filepath.Join(h, ".vscode/argv.json"), "{}")
	write(t, filepath.Join(h, "Library/Application Support/Code/User/settings.json"), "{}")
	write(t, filepath.Join(h, "Library/Application Support/JetBrains/IntelliJIdea2024.2/options/x.xml"), "x")

	return m
}

func (m *machine) provider() *Provider {
	return &Provider{
		home:      m.home,
		appDirs:   []string{m.apps},
		hasCLI:    func(name string) bool { return slices.Contains(m.clis, name) },
		processes: func(context.Context) []string { return m.processes },
		tools:     tools,
		// macOS says Claude was last opened on Nov 10, 2026; nothing else.
		appLastUsed: func(app string) time.Time {
			if filepath.Base(app) == "Claude.app" {
				return claudeOpened
			}
			return time.Time{}
		},
		historyMin: 10_000_000,
	}
}

// Dates the test machine knows.
var (
	claudeOpened   = time.Date(2026, 11, 10, 20, 18, 0, 0, time.UTC) // Claude.app, from macOS
	codexSession   = time.Date(2026, 10, 25, 11, 5, 0, 0, time.UTC)  // the newest Codex session
	vscodeLastFile = time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)     // the newest file VS Code left
)

// touch sets a file's modification time.
func touch(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func byName(items []core.Item, name string) core.Item {
	i := slices.IndexFunc(items, func(it core.Item) bool { return it.Name == name })
	if i < 0 {
		return core.Item{}
	}
	return items[i]
}

func TestScan(t *testing.T) {
	ctx := context.Background()

	t.Run("each finding lands in the right category", func(t *testing.T) {
		items, err := newMachine(t).provider().Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		providertest.CheckCategories(t, New(), items)

		want := map[string]string{
			"VS Code · .vscode":                                                catLeftovers,
			"VS Code · Application Support/Code":                               catLeftovers,
			"IntelliJ IDEA · Application Support/JetBrains/IntelliJIdea2024.2": catLeftovers,
			"Claude Code · 2.1.220":                                            catOldVersions,
			"Claude Code · 2.1.251":                                            catOldVersions,
			"Cursor · eamodio.gitlens-19.1.0":                                  catOldVersions,
			"Cursor · CachedData":                                              catCaches,
			"Codex · .codex/.tmp":                                              catCaches,
			"Codex · codex-primary-runtime":                                    catRuntimes,
			"Claude · Application Support/Claude":                              catInfo,
			"Claude Code · history · 1 more project":                           catInfo,
		}
		for name, cat := range want {
			if got := byName(items, name).CategoryID; got != cat {
				t.Errorf("%q in %q, want %q", name, got, cat)
			}
		}
		if len(items) != len(want) {
			var got []string
			for _, it := range items {
				got = append(got, it.Name)
			}
			t.Errorf("got %d items, want %d: %v", len(items), len(want), got)
		}
	})

	t.Run("current versions, settings and history are never offered", func(t *testing.T) {
		items, _ := newMachine(t).provider().Scan(ctx)
		for _, unwanted := range []string{"Claude Code · 2.1.296", "Cursor · eamodio.gitlens-19.2.0", "Cursor · User", "Codex · .codex/sessions"} {
			if byName(items, unwanted).ID != "" {
				t.Errorf("%q must not be offered", unwanted)
			}
		}
	})

	t.Run("data that apps manage themselves is shown but protected", func(t *testing.T) {
		items, _ := newMachine(t).provider().Scan(ctx)
		ok, why := byName(items, "Claude · Application Support/Claude").CanSelect()
		if ok || why.Key != reasonClaudeApp {
			t.Errorf("Claude app data: ok = %v, why = %+v", ok, why)
		}
	})

	t.Run("caches of a running app are blocked, its old versions are not", func(t *testing.T) {
		m := newMachine(t)
		m.processes = []string{"/Applications/Cursor.app/Contents/MacOS/Cursor"}
		items, _ := m.provider().Scan(ctx)

		if ok, _ := byName(items, "Cursor · CachedData").CanSelect(); ok {
			t.Error("Cursor cache should be blocked while Cursor runs")
		}
		if ok, _ := byName(items, "Cursor · eamodio.gitlens-19.1.0").CanSelect(); !ok {
			t.Error("old extension should stay selectable")
		}
	})

	t.Run("an old version that is still running is blocked", func(t *testing.T) {
		m := newMachine(t)
		m.processes = []string{filepath.Join(m.home, ".local/share/claude/versions/2.1.251") + " --resume"}
		items, _ := m.provider().Scan(ctx)

		ok, why := byName(items, "Claude Code · 2.1.251").CanSelect()
		if ok || why.Key != reasonVersionRunning {
			t.Errorf("running 2.1.251: ok = %v, why = %+v", ok, why)
		}
		if ok, _ := byName(items, "Claude Code · 2.1.220").CanSelect(); !ok {
			t.Error("2.1.220 is not running and should be selectable")
		}
	})

	t.Run("an installed IDE keeps its newest version folder", func(t *testing.T) {
		m := newMachine(t)
		write(t, filepath.Join(m.home, "Library/Application Support/JetBrains/IntelliJIdea2025.1/options/x.xml"), "x")
		write(t, filepath.Join(m.apps, "IntelliJ IDEA.app", "Contents", "Info.plist"), "")

		items, _ := m.provider().Scan(ctx)

		if got := byName(items, "IntelliJ IDEA · Application Support/JetBrains/IntelliJIdea2024.2").CategoryID; got != catOldVersions {
			t.Errorf("2024.2 category = %q, want old versions", got)
		}
		if byName(items, "IntelliJ IDEA · Application Support/JetBrains/IntelliJIdea2025.1").ID != "" {
			t.Error("the newest version folder must not be offered")
		}
	})
}

func TestLastUsed(t *testing.T) {
	ctx := context.Background()

	t.Run("each row says when its tool was last used", func(t *testing.T) {
		// Given: dates for Claude (from macOS), Codex (its newest session)
		// and VS Code (removed: the newest file it left behind)
		m := newMachine(t)
		touch(t, filepath.Join(m.home, ".codex/sessions/chat.jsonl"), codexSession)
		for _, f := range []string{".vscode/argv.json", ".vscode/extensions/ms-python.python-2024.1.0/package.json"} {
			touch(t, filepath.Join(m.home, f), vscodeLastFile.Add(-time.Hour))
		}
		touch(t, filepath.Join(m.home, ".vscode/argv.json"), vscodeLastFile)

		// When
		items, _ := m.provider().Scan(ctx)

		// Then
		for name, want := range map[string]time.Time{
			"Claude · Application Support/Claude": claudeOpened,
			"Codex · .codex/.tmp":                 codexSession,
			"Codex · codex-primary-runtime":       codexSession,
			"VS Code · .vscode":                   vscodeLastFile,
		} {
			if got := byName(items, name).LastUsed; !got.Equal(want) {
				t.Errorf("%q last used %v, want %v", name, got, want)
			}
		}
	})

	t.Run("a big project's history gets its own row, named after the project", func(t *testing.T) {
		// Given: a project with 11 MB of history. Its session first mentions a
		// worktree of another project (sessions can move around), then its own folder.
		m := newMachine(t)
		cwd := filepath.Join(kit.Home(), "Projects", "courses")
		other := filepath.Join(kit.Home(), "Projects", "app", ".claude", "worktrees", "feature-x")
		dir := filepath.Join(m.home, ".claude/projects", encodeRe.ReplaceAllString(cwd, "-"))
		write(t, filepath.Join(dir, "a.jsonl"),
			`{"type":"user","cwd":"`+other+`","message":"hi"}`+"\n"+`{"type":"user","cwd":"`+cwd+`"}`+"\n")
		write(t, filepath.Join(dir, "big.bin"), strings.Repeat("x", 11_000_000))
		lastSession := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
		touch(t, filepath.Join(dir, "a.jsonl"), lastSession)
		touch(t, filepath.Join(dir, "big.bin"), lastSession.Add(-time.Hour))

		// When
		items, _ := m.provider().Scan(ctx)

		// Then: its own protected row with the real folder and date; the small
		// project from newMachine is in the row for the rest
		row := byName(items, "Claude Code · history · ~/Projects/courses")
		if row.Size < 11_000_000 || !row.LastUsed.Equal(lastSession) || row.Protected == nil {
			t.Errorf("history row = %+v", row)
		}
		if byName(items, "Claude Code · history · 1 more project").ID == "" {
			t.Error("the small project's history should be in the row for the rest")
		}
	})
}

func TestPlanNeverRemovesAHomeFolderItself(t *testing.T) {
	// Given: VS Code leftovers in ~/.vscode, a folder directly in the home folder
	m := newMachine(t)
	p := m.provider()
	items, _ := p.Scan(context.Background())

	// When
	actions, err := p.Plan([]core.Item{byName(items, "VS Code · .vscode")})
	if err != nil {
		t.Fatal(err)
	}

	// Then: the contents are deleted (two levels deeper than home), not the folder itself
	if len(actions) != 2 {
		t.Fatalf("actions = %v, want 2 (argv.json and extensions)", actions)
	}
	for _, a := range actions {
		if filepath.Dir(a.RemovePath) != filepath.Join(m.home, ".vscode") {
			t.Errorf("unexpected path %s", a.RemovePath)
		}
	}
}

func TestLibraryCachesHaveOneOwner(t *testing.T) {
	// Every ~/Library/Caches folder from the tools table must be
	// in kit.LibraryCachesOwned, otherwise "App caches & logs" will show it too.
	for _, tl := range tools {
		for _, path := range append(append([]string{}, tl.data...), tl.caches...) {
			if name, ok := strings.CutPrefix(path, caches); ok && !kit.LibraryCachesOwned[name] {
				t.Errorf("%s (%s) is not in kit.LibraryCachesOwned", path, tl.name)
			}
			if name, ok := strings.CutPrefix(path, "Library/Logs/"); ok && !kit.LibraryLogsOwned[name] {
				t.Errorf("%s (%s) is not in kit.LibraryLogsOwned", path, tl.name)
			}
		}
	}
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(), reasonRunning, reasonVersionRunning, reasonClaudeApp, reasonClaudeHistory)
}

func TestProjectName(t *testing.T) {
	// A worktree's history reads as the project and the worktree's name.
	cwd := filepath.Join(kit.Home(), "Projects", "app", ".claude", "worktrees", "feature-x")
	dir := filepath.Join(t.TempDir(), encodeRe.ReplaceAllString(cwd, "-"))
	write(t, filepath.Join(dir, "s.jsonl"), `{"cwd":"`+cwd+`"}`)

	if got := projectName(dir); got != "~/Projects/app › feature-x" {
		t.Errorf("projectName = %q", got)
	}

	// No session mentions the folder: its own name is shown as is.
	if got := projectName(filepath.Join(t.TempDir(), "-Users-me-x")); got != "-Users-me-x" {
		t.Errorf("projectName without sessions = %q", got)
	}
}
