package appcaches

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/providertest"
)

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeHome is caches and logs like on this Mac. The minimum sizes are removed
// so that small test files make it into the list.
func fakeHome(t *testing.T, processes ...string) *Provider {
	t.Helper()
	home := t.TempDir()
	caches := filepath.Join(home, "Library", "Caches")

	write(t, filepath.Join(caches, "Google", "Chrome", "Default", "Cache", "data_0"))
	write(t, filepath.Join(caches, "com.spotify.client", "Data", "x"))
	write(t, filepath.Join(caches, "com.todesktop.230313mzl4w4u92.ShipIt", "update.zip"))
	write(t, filepath.Join(caches, "orca-updater", "pending.zip"))
	write(t, filepath.Join(caches, "CocoaPods", "Pods", "x"))              // owned by "Package caches"
	write(t, filepath.Join(caches, "com.apple.Safari", "x"))               // Apple system cache
	write(t, filepath.Join(caches, "com.anthropic.claudefordesktop", "x")) // Claude cache
	write(t, filepath.Join(caches, "claude-cli-nodejs", "x"))              // Claude Code cache
	write(t, filepath.Join(caches, "GeoServices", "x"))                    // Apple maps
	write(t, filepath.Join(home, "Library", "Logs", "Homebrew", "brew.log"))
	write(t, filepath.Join(home, "Library", "Logs", "Claude", "main.log")) // Claude logs

	return &Provider{
		home:      home,
		processes: func(context.Context) []string { return processes },
		minCache:  1,
		minLog:    1,
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

	t.Run("finds app caches, update leftovers and logs in their categories", func(t *testing.T) {
		items, err := fakeHome(t).Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		providertest.CheckCategories(t, New(), items)

		want := map[string]string{
			"Google":                               catCaches,
			"com.spotify.client":                   catCaches,
			"com.todesktop.230313mzl4w4u92.ShipIt": catUpdates,
			"orca-updater":                         catUpdates,
			"Homebrew":                             catLogs,
		}
		if len(items) != len(want) {
			t.Errorf("got %d items, want %d: %v", len(items), len(want), items)
		}
		for name, cat := range want {
			if got := byName(items, name).CategoryID; got != cat {
				t.Errorf("%q in %q, want %q", name, got, cat)
			}
		}
	})

	t.Run("caches owned elsewhere, Apple system caches and Claude are skipped", func(t *testing.T) {
		items, _ := fakeHome(t).Scan(ctx)
		for _, skipped := range []string{"CocoaPods", "com.apple.Safari", "com.anthropic.claudefordesktop", "claude-cli-nodejs", "GeoServices", "Claude"} {
			if byName(items, skipped).ID != "" {
				t.Errorf("%q should not be listed", skipped)
			}
		}
	})

	t.Run("cache of a running app cannot be selected", func(t *testing.T) {
		items, _ := fakeHome(t,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome --type=renderer",
			"/Applications/Spotify.app/Contents/MacOS/Spotify",
		).Scan(ctx)

		for _, name := range []string{"Google", "com.spotify.client"} {
			ok, why := byName(items, name).CanSelect()
			if ok || why.Key != reasonRunning {
				t.Errorf("%q: ok = %v, why = %+v", name, ok, why)
			}
		}
		if ok, _ := byName(items, "orca-updater").CanSelect(); !ok {
			t.Error("orca-updater should stay selectable")
		}
	})
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(), reasonRunning)
}
