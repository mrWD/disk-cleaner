package xcode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"disk-cleaner/internal/providers/providertest"
)

// fakeHome creates a temporary "home folder" with Xcode files
// and returns a provider that looks into it.
func fakeHome(t *testing.T, folders ...string) *Provider {
	t.Helper()
	home := t.TempDir()

	for _, f := range folders {
		dir := filepath.Join(home, "Library", "Developer", "Xcode", f)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "data"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return &Provider{home: home}
}

func TestScan(t *testing.T) {
	ctx := context.Background()

	t.Run("finds Xcode folders and names them for people", func(t *testing.T) {
		// Given: a home folder with typical Xcode files
		p := fakeHome(t,
			"DerivedData/Lingary-bxqjcmhlwrsvdyfkqlfxonbsnjcn",
			"DerivedData/ModuleCache.noindex",
			"Archives/2026-11-08/ITSkills 08.11.2026, 14.20.xcarchive",
			"iOS DeviceSupport/iPhone17,1 26.0 (23A341)",
			"DocumentationCache",
		)

		// When
		items, err := p.Scan(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Then: every folder became an item of its own category with a readable name
		want := map[string]string{
			"Lingary":                        catDerivedData,
			"ModuleCache.noindex":            catDerivedData,
			"ITSkills 08.11.2026, 14.20":     catArchives,
			"iOS · iPhone17,1 26.0 (23A341)": catDeviceSupport,
			"DocumentationCache":             catDocumentation,
		}
		if len(items) != len(want) {
			t.Fatalf("got %d items, want %d: %+v", len(items), len(want), items)
		}
		for _, it := range items {
			if want[it.Name] != it.CategoryID {
				t.Errorf("item %q in %q, want %q", it.Name, it.CategoryID, want[it.Name])
			}
			if it.Size <= 0 {
				t.Errorf("item %q has no size", it.Name)
			}
		}

		providertest.CheckCategories(t, p, items)
	})

	t.Run("no Xcode folders means nothing to show", func(t *testing.T) {
		items, err := fakeHome(t).Scan(ctx)
		if err != nil || len(items) != 0 {
			t.Errorf("items = %v, err = %v", items, err)
		}
	})

	t.Run("plan removes exactly the found folders", func(t *testing.T) {
		p := fakeHome(t, "DerivedData/App-bxqjcmhlwrsvdyfkqlfxonbsnjcn")
		items, _ := p.Scan(ctx)

		actions, err := p.Plan(items)

		if err != nil || len(actions) != 1 || actions[0].RemovePath != items[0].ID {
			t.Errorf("actions = %v, err = %v", actions, err)
		}
	})
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New())
}
