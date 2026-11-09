package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/providers/providertest"
	"disk-cleaner/internal/shell"
)

// git запускает настоящий git (он есть на любом Mac с Xcode).
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	// -c user.name/email — чтобы коммит не требовал настроек машины.
	full := append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)
	if out, err := shell.Run(context.Background(), "/usr/bin/git", full...); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeProjects — папка проектов с репозиторием courses и его worktree
// в courses/.claude/worktrees/merge-main, как у Claude Code.
func fakeProjects(t *testing.T) (root, wt string) {
	t.Helper()

	// EvalSymlinks: на macOS временные папки лежат за ссылкой /var → /private/var,
	// а git возвращает «настоящие» пути. Приводим к одному виду.
	root, _ = filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(root, "courses")

	write(t, filepath.Join(repo, ".gitignore"), "node_modules/\nPods/\n")
	write(t, filepath.Join(repo, "build", "config.txt"), "tracked build folder") // build НЕ игнорируется
	git(t, root, "init", "-q", repo)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "init")

	wt = filepath.Join(repo, ".claude", "worktrees", "merge-main")
	git(t, repo, "worktree", "add", "-q", "-b", "main-next", wt)

	// Артефакты в worktree: игнорируемые node_modules и ios/Pods,
	// и отслеживаемая папка build — её трогать нельзя.
	write(t, filepath.Join(wt, "node_modules", "react", "index.js"), "x")
	write(t, filepath.Join(wt, "ios", "Pods", "Manifest.lock"), "x")
	write(t, filepath.Join(wt, "build", "out.txt"), "x")

	// node_modules основного репозитория — это не worktree, его не трогаем.
	write(t, filepath.Join(repo, "node_modules", "react", "index.js"), "x")

	return root, wt
}

func newProvider(root string, processes ...string) *Provider {
	return &Provider{
		roots:     func() []string { return []string{root} },
		run:       shell.Run,
		processes: func(context.Context) []string { return processes },
		minSize:   1,
	}
}

func names(items []core.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Name)
	}
	slices.Sort(out)
	return out
}

func TestScan(t *testing.T) {
	ctx := context.Background()

	t.Run("only git-ignored folders inside worktrees are offered", func(t *testing.T) {
		root, _ := fakeProjects(t)

		items, err := newProvider(root).Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Then: node_modules и ios/Pods из worktree; ни отслеживаемого build,
		// ни node_modules основного репозитория.
		want := []string{"merge-main · ios/Pods", "merge-main · node_modules"}
		if got := names(items); !slices.Equal(got, want) {
			t.Errorf("items = %v, want %v", got, want)
		}
		providertest.CheckCategories(t, New(nil), items)
	})

	t.Run("detail shows where the worktree is and its branch", func(t *testing.T) {
		root, wt := fakeProjects(t)

		items, _ := newProvider(root).Scan(ctx)

		if len(items) == 0 || items[0].Detail != kit.Tilde(wt)+" · main-next" {
			t.Errorf("detail = %q", items[0].Detail)
		}
	})

	t.Run("folders of a worktree with a running dev server are in use", func(t *testing.T) {
		root, wt := fakeProjects(t)
		metro := "node " + wt + "/node_modules/.bin/react-native start"

		items, _ := newProvider(root, metro).Scan(ctx)

		for _, it := range items {
			ok, why := it.CanSelect()
			if ok || why.Key != reasonProcessRunning || why.Args["Process"] != "node" {
				t.Errorf("%q should be blocked by the running node process, got %+v", it.Name, why)
			}
		}
	})
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(nil), reasonProcessRunning)
}
