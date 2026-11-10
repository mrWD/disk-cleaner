package packagecaches

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/providers/providertest"
)

// fakeHome is a temporary home folder with caches and a fake process list.
func fakeHome(t *testing.T, processes []string, caches ...string) *Provider {
	t.Helper()
	home := t.TempDir()

	for _, c := range caches {
		dir := filepath.Join(home, c)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pkg.tgz"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return &Provider{
		home:      home,
		processes: func(context.Context) []string { return processes },
	}
}

func TestScan(t *testing.T) {
	ctx := context.Background()

	t.Run("finds existing caches and skips missing ones", func(t *testing.T) {
		// Given: there are only npm and Gradle caches
		p := fakeHome(t, nil, ".npm/_cacache", ".gradle/caches")

		// When
		items, _ := p.Scan(ctx)

		// Then: exactly two items, both can be selected
		if len(items) != 2 {
			t.Fatalf("items = %+v, want npm and Gradle", items)
		}
		for _, it := range items {
			if ok, _ := it.CanSelect(); !ok {
				t.Errorf("%q should be selectable", it.Name)
			}
		}
		providertest.CheckCategories(t, p, items)
	})

	t.Run("Gradle cache cannot be cleaned while a Gradle daemon runs", func(t *testing.T) {
		// Given: among the processes there is a Gradle daemon
		daemon := "/usr/bin/java -Xmx2g org.gradle.launcher.daemon.bootstrap.GradleDaemon 8.10"
		p := fakeHome(t, []string{daemon}, ".gradle/caches", ".npm/_cacache")

		// When
		items, _ := p.Scan(ctx)

		// Then: Gradle is Running, npm can still be selected
		for _, it := range items {
			wantRunning := it.CategoryID == "packagecaches.gradle"
			if (it.Status == core.StatusRunning) != wantRunning {
				t.Errorf("%q status = %v", it.Name, it.Status)
			}
		}
	})
}

func TestNpxAndGradleJDKArchives(t *testing.T) {
	// Given: an npx cache and two JDK archives from Gradle next to an unpacked JDK
	p := fakeHome(t, nil, ".npm/_npx/abc123", ".gradle/jdks/eclipse_adoptium-17-aarch64-os_x.2")
	for _, name := range []string{"OpenJDK17U-jdk.tar.gz", "OpenJDK21U-jdk.tar.gz"} {
		if err := os.WriteFile(filepath.Join(p.home, ".gradle", "jdks", name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// When
	items, _ := p.Scan(context.Background())

	// Then: npx and both archives are there, but the unpacked JDK is not
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	slices.Sort(names)
	want := []string{"Gradle JDK download · OpenJDK17U-jdk.tar.gz", "Gradle JDK download · OpenJDK21U-jdk.tar.gz", "npx cache"}
	if !slices.Equal(names, want) {
		t.Errorf("items = %v, want %v", names, want)
	}
}

func TestLibraryCachesHaveOneOwner(t *testing.T) {
	// Every ~/Library/Caches folder of this provider must be in the shared
	// kit.LibraryCachesOwned list, otherwise "App caches & logs" will show it too.
	for _, c := range caches {
		for _, f := range c.folders {
			dir, name := filepath.Split(f.path)
			if filepath.Clean(dir) == filepath.Join("Library", "Caches") && !kit.LibraryCachesOwned[name] {
				t.Errorf("%s is not in kit.LibraryCachesOwned", f.path)
			}
		}
	}
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(), reasonGradleDaemon)
}
