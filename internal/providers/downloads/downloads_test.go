package downloads

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/providertest"
)

// write creates a file a bit over 1 MB (the section does not show smaller ones).
func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 1_100_000)
	for i := range data {
		data[i] = 1
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeDownloads is Downloads like in Viktor's screenshot, plus an archive, an iOS
// app build, a small file and a regular document.
func fakeDownloads(t *testing.T, hdiutil string) *Provider {
	t.Helper()
	dir := t.TempDir()

	write(t, filepath.Join(dir, "LM-Studio-0.4.26-4-arm64.dmg"))
	write(t, filepath.Join(dir, "Lingary-191.aab"))
	write(t, filepath.Join(dir, "Xcode_27.1.xip"))
	write(t, filepath.Join(dir, "Lingary.xcarchive", "Info.plist"))
	write(t, filepath.Join(dir, "report.pdf")) // a document: not ours
	if err := os.WriteFile(filepath.Join(dir, "tiny.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	return &Provider{
		dir: dir,
		run: func(context.Context, string, ...string) ([]byte, error) { return []byte(hdiutil), nil },
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

	t.Run("installers, builds and archives land in their categories", func(t *testing.T) {
		items, err := fakeDownloads(t, "").Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		providertest.CheckCategories(t, New(nil), items)

		want := map[string]string{
			"LM-Studio-0.4.26-4-arm64.dmg": catInstallers,
			"Lingary-191.aab":              catBuilds,
			"Lingary.xcarchive":            catBuilds,
			"Xcode_27.1.xip":               catArchives,
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

	t.Run("documents and tiny files are not offered", func(t *testing.T) {
		items, _ := fakeDownloads(t, "").Scan(ctx)
		for _, name := range []string{"report.pdf", "tiny.zip"} {
			if byName(items, name).ID != "" {
				t.Errorf("%q must not be offered", name)
			}
		}
	})

	t.Run("a mounted disk image cannot be selected", func(t *testing.T) {
		p := fakeDownloads(t, "")
		dmg := filepath.Join(p.dir, "LM-Studio-0.4.26-4-arm64.dmg")
		p.run = func(context.Context, string, ...string) ([]byte, error) {
			return []byte("================================================\nimage-path      : " + dmg + "\nimage-alias     : " + dmg + "\n"), nil
		}

		items, _ := p.Scan(ctx)

		ok, why := byName(items, "LM-Studio-0.4.26-4-arm64.dmg").CanSelect()
		if ok || why.Key != reasonMounted {
			t.Errorf("mounted dmg: ok = %v, why = %+v", ok, why)
		}
	})

	t.Run("no access to Downloads is explained, not an error", func(t *testing.T) {
		p := fakeDownloads(t, "")
		if err := os.Chmod(p.dir, 0o000); err != nil { // this is what a macOS denial looks like
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p.dir, 0o755) })

		_, err := p.Scan(ctx)

		var u *core.UnavailableError
		if !errors.As(err, &u) || u.Reason.Key != reasonNoAccess {
			t.Errorf("err = %v, want unavailable(no access)", err)
		}
	})
}

func TestPlanMovesToTrash(t *testing.T) {
	p := fakeDownloads(t, "")
	items, _ := p.Scan(context.Background())

	actions, _ := p.Plan(items)

	for _, a := range actions {
		if !a.Trash {
			t.Errorf("%s would be deleted for good, want moved to the Trash", a.RemovePath)
		}
	}
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(nil), reasonNoAccess, reasonMounted, reasonTurnedOff)
}

func TestTurnedOff(t *testing.T) {
	// Given: Downloads is switched off in Settings
	p := &Provider{dir: t.TempDir(), enabled: func() bool { return false }}

	// When
	items, err := p.Scan(context.Background())

	// Then: the section says it's off instead of scanning
	var unavailable *core.UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason.Key != reasonTurnedOff || len(items) != 0 {
		t.Errorf("items = %v, err = %v; want Unavailable(%s)", items, err, reasonTurnedOff)
	}
}
