package android

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/providertest"
)

// write создаёт файл с содержимым (и все папки до него).
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeSDK — временные SDK и папка эмуляторов, как на этом Mac:
// эмулятор Lingary_Test на android-35, лишний образ android-34, два NDK
// и исходники android-35.
func fakeSDK(t *testing.T, processes ...string) *Provider {
	t.Helper()
	home := t.TempDir()
	p := &Provider{
		sdk:       filepath.Join(home, "sdk"),
		avdHome:   filepath.Join(home, "avd"),
		processes: func(context.Context) []string { return processes },
	}

	write(t, filepath.Join(p.avdHome, "Lingary_Test.avd", "config.ini"),
		"hw.ramSize=4096\nimage.sysdir.1=system-images/android-35/google_apis_playstore/arm64-v8a/\n")
	write(t, filepath.Join(p.avdHome, "Lingary_Test.ini"), "path=...\n")
	write(t, filepath.Join(p.sdk, "system-images", "android-35", "google_apis_playstore", "arm64-v8a", "system.img"), "x")
	write(t, filepath.Join(p.sdk, "system-images", "android-34", "google_apis", "arm64-v8a", "system.img"), "x")
	write(t, filepath.Join(p.sdk, "ndk", "27.0.12077973", "source.properties"), "x")
	write(t, filepath.Join(p.sdk, "ndk", "27.1.12297006", "source.properties"), "x")
	write(t, filepath.Join(p.sdk, "sources", "android-35", "android", "app", "Activity.java"), "x")

	return p
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

	t.Run("system image used by an emulator is in use, unused one is not", func(t *testing.T) {
		items, _ := fakeSDK(t).Scan(ctx)
		providertest.CheckCategories(t, New(), items)

		_, why := byName(items, "android-35 · google_apis_playstore · arm64-v8a").CanSelect()
		if why == nil || why.Key != reasonUsedByEmulator || why.Args["Emulator"] != "Lingary_Test" {
			t.Errorf("android-35 image should be blocked as used by Lingary_Test, got %+v", why)
		}
		if ok, _ := byName(items, "android-34 · google_apis · arm64-v8a").CanSelect(); !ok {
			t.Error("android-34 image is unused and should be selectable")
		}
		if byName(items, "NDK 27.0.12077973").ID == "" || byName(items, "NDK 27.1.12297006").ID == "" {
			t.Error("both NDK versions should be found")
		}
	})

	t.Run("SDK sources are found and safe to select", func(t *testing.T) {
		items, _ := fakeSDK(t).Scan(ctx)

		src := byName(items, "Sources android-35")
		if src.CategoryID != catSources {
			t.Fatalf("sources android-35 not found, items = %v", items)
		}
		if ok, _ := src.CanSelect(); !ok {
			t.Error("sources should be selectable")
		}
	})

	t.Run("running emulator cannot be selected", func(t *testing.T) {
		items, _ := fakeSDK(t, "/Users/me/Library/Android/sdk/emulator/qemu/darwin-aarch64/qemu-system-aarch64 -avd Lingary_Test -no-snapshot").Scan(ctx)

		if byName(items, "Lingary_Test").Status != core.StatusRunning {
			t.Error("Lingary_Test is running and should be Running")
		}
	})

	t.Run("deleting an emulator also deletes its .ini file", func(t *testing.T) {
		p := fakeSDK(t)
		items, _ := p.Scan(ctx)

		actions, _ := p.Plan([]core.Item{byName(items, "Lingary_Test")})

		if len(actions) != 2 || actions[1].RemovePath != filepath.Join(p.avdHome, "Lingary_Test.ini") {
			t.Errorf("actions = %v", actions)
		}
	})

	t.Run("no Android SDK means nothing to show", func(t *testing.T) {
		p := &Provider{sdk: t.TempDir(), avdHome: t.TempDir(), processes: func(context.Context) []string { return nil }}
		items, err := p.Scan(ctx)
		if err != nil || len(items) != 0 {
			t.Errorf("items = %v, err = %v", items, err)
		}
	})
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(), reasonUsedByEmulator)
}
