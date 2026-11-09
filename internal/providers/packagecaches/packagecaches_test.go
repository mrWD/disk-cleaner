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

// fakeHome — временная домашняя папка с кэшами и фейковый список процессов.
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
		// Given: есть только кэши npm и Gradle
		p := fakeHome(t, nil, ".npm/_cacache", ".gradle/caches")

		// When
		items, _ := p.Scan(ctx)

		// Then: ровно два объекта, оба можно выбрать
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
		// Given: среди процессов есть демон Gradle
		daemon := "/usr/bin/java -Xmx2g org.gradle.launcher.daemon.bootstrap.GradleDaemon 8.10"
		p := fakeHome(t, []string{daemon}, ".gradle/caches", ".npm/_cacache")

		// When
		items, _ := p.Scan(ctx)

		// Then: Gradle — Running, npm по-прежнему можно выбрать
		for _, it := range items {
			wantRunning := it.CategoryID == "packagecaches.gradle"
			if (it.Status == core.StatusRunning) != wantRunning {
				t.Errorf("%q status = %v", it.Name, it.Status)
			}
		}
	})
}

func TestNpxAndGradleJDKArchives(t *testing.T) {
	// Given: кэш npx и два архива JDK от Gradle рядом с распакованным JDK
	p := fakeHome(t, nil, ".npm/_npx/abc123", ".gradle/jdks/eclipse_adoptium-17-aarch64-os_x.2")
	for _, name := range []string{"OpenJDK17U-jdk.tar.gz", "OpenJDK21U-jdk.tar.gz"} {
		if err := os.WriteFile(filepath.Join(p.home, ".gradle", "jdks", name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// When
	items, _ := p.Scan(context.Background())

	// Then: npx и оба архива есть, а распакованного JDK — нет
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
	// Каждая папка ~/Library/Caches этого провайдера должна быть в общем
	// списке kit.LibraryCachesOwned — иначе её покажет ещё и «Кэши приложений».
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
