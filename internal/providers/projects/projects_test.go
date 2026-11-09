package projects

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/providertest"
	"disk-cleaner/internal/shell"
)

// git запускает настоящий git. Дата коммита задаётся через переменные
// окружения GIT_*_DATE — так можно сделать «старый» проект.
func git(t *testing.T, date, dir string, args ...string) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_DATE", date)
	t.Setenv("GIT_COMMITTER_DATE", date)
	full := append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)
	if out, err := shell.Run(context.Background(), "/usr/bin/git", full...); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("node_modules/\n.venv/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo создаёт репозиторий с одним коммитом на дату date.
func repo(t *testing.T, dir, date string) {
	t.Helper()
	write(t, filepath.Join(dir, ".gitignore"))
	write(t, filepath.Join(dir, "build", "tracked.txt")) // build отслеживается — не трогать
	git(t, date, filepath.Dir(dir), "init", "-q", dir)
	git(t, date, dir, "add", ".")
	git(t, date, dir, "commit", "-q", "-m", "init")
	write(t, filepath.Join(dir, "node_modules", "react", "index.js"))
}

// today — «сейчас» в тестах.
var today = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// fakeProjects — папка проектов: давно заброшенный meal-plan, активный
// courses с worktree внутри и вложенный appose2/frontend.
func fakeProjects(t *testing.T, processes ...string) (*Provider, string) {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())

	repo(t, filepath.Join(root, "meal-plan"), "2026-09-25T10:00:00")
	repo(t, filepath.Join(root, "courses"), "2026-11-08T10:00:00")
	repo(t, filepath.Join(root, "appose2", "frontend"), "2026-10-01T10:00:00")

	// Worktree внутри courses — его node_modules принадлежат провайдеру worktrees.
	wt := filepath.Join(root, "courses", ".claude", "worktrees", "merge-main")
	git(t, "2026-11-08T10:00:00", filepath.Join(root, "courses"), "worktree", "add", "-q", "-b", "next", wt)
	write(t, filepath.Join(wt, "node_modules", "react", "index.js"))

	p := &Provider{
		roots:     func() []string { return []string{root} },
		run:       shell.Run,
		processes: func(context.Context) []string { return processes },
		now:       func() time.Time { return today },
		minSize:   1,
	}
	return p, root
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

	t.Run("projects without commits for 30 days are inactive", func(t *testing.T) {
		p, _ := fakeProjects(t)
		items, err := p.Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		providertest.CheckCategories(t, New(nil), items)

		if got := byName(items, "meal-plan · node_modules").CategoryID; got != catInactive {
			t.Errorf("meal-plan category = %q, want inactive", got)
		}
		if got := byName(items, "appose2/frontend · node_modules").CategoryID; got != catInactive {
			t.Errorf("appose2/frontend category = %q, want inactive", got)
		}
		if got := byName(items, "courses · node_modules").CategoryID; got != catActive {
			t.Errorf("courses category = %q, want active", got)
		}
	})

	t.Run("tracked folders and worktrees are not offered", func(t *testing.T) {
		p, _ := fakeProjects(t)
		items, _ := p.Scan(ctx)

		for _, it := range items {
			if filepath.Base(it.ID) == "build" {
				t.Errorf("tracked build folder offered: %s", it.ID)
			}
			if filepath.Base(filepath.Dir(it.ID)) == "merge-main" {
				t.Errorf("worktree folder offered by projects: %s", it.ID)
			}
		}
		if len(items) != 3 {
			t.Errorf("got %d items, want 3 node_modules: %v", len(items), items)
		}
	})

	t.Run("detail shows the last commit date", func(t *testing.T) {
		p, root := fakeProjects(t)
		items, _ := p.Scan(ctx)

		want := filepath.Join(root, "meal-plan") + " · 2026-09-25"
		if got := byName(items, "meal-plan · node_modules").Detail; got != want {
			t.Errorf("detail = %q, want %q", got, want)
		}
	})

	t.Run("folders of a project with a running dev server are in use", func(t *testing.T) {
		p, root := fakeProjects(t)
		metro := "node " + filepath.Join(root, "courses") + "/node_modules/.bin/expo start"
		p.processes = func(context.Context) []string { return []string{metro} }

		items, _ := p.Scan(ctx)

		ok, why := byName(items, "courses · node_modules").CanSelect()
		if ok || why.Key != reasonProcessRunning {
			t.Errorf("courses: ok = %v, why = %+v", ok, why)
		}
		if ok, _ := byName(items, "meal-plan · node_modules").CanSelect(); !ok {
			t.Error("meal-plan should stay selectable")
		}
	})
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(nil), reasonProcessRunning)
}

func TestLeftoverBuilds(t *testing.T) {
	ctx := context.Background()

	// Given: проект, где рядом с кодом лежат архив Xcode (git его не знает)
	// и демо-APK, закоммиченный в репозиторий; плюс release/ от electron-builder,
	// который git игнорирует.
	root, _ := filepath.EvalSymlinks(t.TempDir())
	dir := filepath.Join(root, "lingary-review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("release/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "docs", "demo.apk"))
	git(t, "2026-11-01T10:00:00", root, "init", "-q", dir)
	git(t, "2026-11-01T10:00:00", dir, "add", ".")
	git(t, "2026-11-01T10:00:00", dir, "commit", "-q", "-m", "init")
	write(t, filepath.Join(dir, "Lingary-191.xcarchive", "Info.plist"))
	write(t, filepath.Join(dir, "release", "Lingary-1.0.0.dmg"))

	p := &Provider{
		roots:     func() []string { return []string{root} },
		run:       shell.Run,
		processes: func(context.Context) []string { return nil },
		now:       func() time.Time { return today },
		minSize:   1,
	}

	// When
	items, err := p.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Then: архив — в «Оставленных сборках», release/ — как папка сборки,
	// а закоммиченный APK не предлагается вовсе.
	if got := byName(items, "lingary-review · Lingary-191.xcarchive").CategoryID; got != catBuilds {
		t.Errorf("xcarchive category = %q, want %q", got, catBuilds)
	}
	if got := byName(items, "lingary-review · release").CategoryID; got != catActive {
		t.Errorf("release/ category = %q, want %q", got, catActive)
	}
	if byName(items, "lingary-review · docs/demo.apk").ID != "" {
		t.Error("tracked demo.apk must not be offered")
	}
}
