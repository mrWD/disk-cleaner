package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"disk-cleaner/internal/core"
)

// newTestExecutor — исполнитель, у которого «домашняя папка» — временная,
// а окно пароля администратора заменено записью команды.
func newTestExecutor(t *testing.T) (*Local, *[]string) {
	t.Helper()

	home := t.TempDir()
	adminRoot := t.TempDir()
	var adminCmds []string

	ex := &Local{
		Home:       home,
		AdminRoots: []string{adminRoot},
		runAdmin: func(_ context.Context, cmd string) error {
			adminCmds = append(adminCmds, cmd)
			return nil
		},
		// Вместо настоящей Корзины — запись в список (и пометка в adminCmds,
		// чтобы тест видел, что вызвали именно Корзину).
		trash: func(_ context.Context, path string) error {
			adminCmds = append(adminCmds, "trash "+path)
			return nil
		},
	}

	return ex, &adminCmds
}

// mkdir создаёт папку с файлом внутри и возвращает её путь.
func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestRemove(t *testing.T) {
	ctx := context.Background()

	t.Run("removes a cache folder deep inside home", func(t *testing.T) {
		ex, _ := newTestExecutor(t)
		cache := mkdir(t, ex.Home, ".npm", "_cacache")

		err := ex.Run(ctx, core.Action{RemovePath: cache})

		if err != nil || exists(cache) {
			t.Errorf("err = %v, exists = %v", err, exists(cache))
		}
	})

	// Сценарии, где исполнитель обязан отказаться.
	refusals := []struct {
		scenario string
		path     func(home string) string
	}{
		{"refuses the home folder itself", func(h string) string { return h }},
		{"refuses a top-level folder of home", func(h string) string { return filepath.Join(h, "Projects") }},
		{"refuses Application Support", func(h string) string { return filepath.Join(h, "Library", "Application Support") }},
		{"refuses all of Library/Caches", func(h string) string { return filepath.Join(h, "Library", "Caches") }},
		{"refuses paths outside home", func(string) string { return "/usr/local/lib" }},
		{"refuses relative paths", func(string) string { return "Library/Caches/x" }},
		{"refuses paths with ..", func(h string) string { return h + "/.npm/../Projects" }},
	}

	for _, r := range refusals {
		t.Run(r.scenario, func(t *testing.T) {
			ex, _ := newTestExecutor(t)
			path := r.path(ex.Home)

			err := ex.Run(ctx, core.Action{RemovePath: path})

			if !errors.Is(err, ErrUnsafePath) {
				t.Errorf("Run(rm %s) err = %v, want ErrUnsafePath", path, err)
			}
		})
	}

	t.Run("missing folder is not an error", func(t *testing.T) {
		ex, _ := newTestExecutor(t)

		err := ex.Run(ctx, core.Action{RemovePath: filepath.Join(ex.Home, ".npm", "gone")})

		if err != nil {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("read-only folders are removed too", func(t *testing.T) {
		// Given: папка без права записи, как в кэше модулей Go
		ex, _ := newTestExecutor(t)
		mod := mkdir(t, ex.Home, "go", "pkg", "mod", "example.com@v1")
		if err := os.Chmod(mod, 0o555); err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(ex.Home, "go", "pkg", "mod")

		// When
		err := ex.Run(ctx, core.Action{RemovePath: root})

		// Then
		if err != nil || exists(root) {
			t.Errorf("err = %v, exists = %v", err, exists(root))
		}
	})

	t.Run("admin removal asks macOS for the password and stays in admin roots", func(t *testing.T) {
		ex, adminCmds := newTestExecutor(t)
		dyld := filepath.Join(ex.AdminRoots[0], "dyld", "it's")
		mkdir(t, dyld)

		err := ex.Run(ctx, core.Action{RemovePath: dyld, NeedsAdmin: true})

		if err != nil {
			t.Fatal(err)
		}
		want := "/bin/rm -rf '" + strings.ReplaceAll(dyld, "'", `'\''`) + "'"
		if len(*adminCmds) != 1 || (*adminCmds)[0] != want {
			t.Errorf("admin commands = %q, want %q", *adminCmds, want)
		}
	})

	t.Run("files of the user go to the Trash, not deleted for good", func(t *testing.T) {
		ex, calls := newTestExecutor(t)
		aab := filepath.Join(ex.Home, "Downloads", "Lingary-191.aab")
		mkdir(t, filepath.Dir(aab))
		if err := os.WriteFile(aab, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		err := ex.Run(ctx, core.Action{RemovePath: aab, Trash: true})

		if err != nil || len(*calls) != 1 || (*calls)[0] != "trash "+aab {
			t.Errorf("err = %v, calls = %v", err, *calls)
		}
		if !exists(aab) {
			t.Error("the file must not be deleted directly")
		}
	})

	t.Run("trash still refuses unsafe paths", func(t *testing.T) {
		ex, calls := newTestExecutor(t)

		err := ex.Run(ctx, core.Action{RemovePath: filepath.Join(ex.Home, "Downloads"), Trash: true})

		if !errors.Is(err, ErrUnsafePath) || len(*calls) != 0 {
			t.Errorf("err = %v, calls = %v", err, *calls)
		}
	})

	t.Run("admin removal outside admin roots is refused", func(t *testing.T) {
		ex, adminCmds := newTestExecutor(t)

		err := ex.Run(ctx, core.Action{RemovePath: "/Library/Preferences", NeedsAdmin: true})

		if !errors.Is(err, ErrUnsafePath) || len(*adminCmds) != 0 {
			t.Errorf("err = %v, admin commands = %v", err, *adminCmds)
		}
	})
}

func TestCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("command runs and its failure is reported", func(t *testing.T) {
		ex, _ := newTestExecutor(t)

		if err := ex.Run(ctx, core.Action{Command: []string{"/bin/echo", "ok"}}); err != nil {
			t.Errorf("echo: %v", err)
		}
		if err := ex.Run(ctx, core.Action{Command: []string{"/bin/ls", "/no/such/dir"}}); err == nil {
			t.Error("ls of a missing dir: want an error")
		}
	})

	t.Run("commands with administrator rights are refused", func(t *testing.T) {
		// С паролем можно только удалять пути внутри AdminRoots — и ничего больше.
		ex, adminCmds := newTestExecutor(t)

		err := ex.Run(ctx, core.Action{Command: []string{"/bin/echo", "a b; c"}, NeedsAdmin: true})

		if !errors.Is(err, ErrAdminCommand) || len(*adminCmds) != 0 {
			t.Errorf("err = %v, admin commands = %q", err, *adminCmds)
		}
	})
}
