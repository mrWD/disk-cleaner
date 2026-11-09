package devtools

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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

// machine — домашняя папка, похожая на эту машину 09.11.2026:
// Cursor, Claude, Claude Code и ChatGPT (Codex) установлены, а VS Code
// и IntelliJ IDEA удалены, но оставили данные.
type machine struct {
	home      string
	apps      string   // папка «Программы»
	clis      []string // установленные команды
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

	// Cursor: одно расширение установлено, его старая версия — нет.
	write(t, filepath.Join(h, ".cursor/extensions/extensions.json"), `[{"relativeLocation": "eamodio.gitlens-19.2.0"}]`)
	write(t, filepath.Join(h, ".cursor/extensions/eamodio.gitlens-19.2.0/package.json"), "{}")
	write(t, filepath.Join(h, ".cursor/extensions/eamodio.gitlens-19.1.0/package.json"), "{}")
	write(t, filepath.Join(h, "Library/Application Support/Cursor/CachedData/x.js"), "x")
	write(t, filepath.Join(h, "Library/Application Support/Cursor/User/settings.json"), "{}") // настройки — не трогать

	// Claude и Claude Code.
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
	write(t, filepath.Join(h, ".codex/sessions/chat.jsonl"), "{}") // история — не мусор

	// Остатки удалённых VS Code и IntelliJ IDEA.
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
			"Claude Code · .claude/projects":                                   catInfo,
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

func TestPlanNeverRemovesAHomeFolderItself(t *testing.T) {
	// Given: остатки VS Code в ~/.vscode — папке прямо в домашней
	m := newMachine(t)
	p := m.provider()
	items, _ := p.Scan(context.Background())

	// When
	actions, err := p.Plan([]core.Item{byName(items, "VS Code · .vscode")})
	if err != nil {
		t.Fatal(err)
	}

	// Then: удаляется содержимое (на два уровня глубже домашней), а не сама папка
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
	// Каждая папка ~/Library/Caches из таблицы инструментов должна быть
	// в kit.LibraryCachesOwned — иначе её покажет ещё и «Кэши приложений».
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
