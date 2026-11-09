package simulators

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/providertest"
)

// runtimesOnNov8 — состояние машины утром 08.11.2026: три записи iOS 27.0
// указывают на один образ (две из них «Unusable»), плюс старые версии.
const runtimesOnNov8 = `{
  "AAA": {"identifier": "AAA", "version": "27.0", "build": "24A434", "state": "Ready", "sizeBytes": 7500000000,
          "path": "/System/Library/AssetsV2/ios27/AssetData", "platformIdentifier": "com.apple.platform.iphonesimulator",
          "runtimeIdentifier": "com.apple.CoreSimulator.SimRuntime.iOS-27-0", "deletable": true},
  "BBB": {"identifier": "BBB", "version": "27.0", "build": "24A434", "state": "Unusable - Cryptex Mount Preferred", "sizeBytes": 7500000000,
          "path": "/System/Library/AssetsV2/ios27/AssetData", "platformIdentifier": "com.apple.platform.iphonesimulator",
          "runtimeIdentifier": "com.apple.CoreSimulator.SimRuntime.iOS-27-0", "deletable": true},
  "CCC": {"identifier": "CCC", "version": "27.0", "build": "24A434", "state": "Unusable - Cryptex Mount Preferred", "sizeBytes": 7500000000,
          "path": "/System/Library/AssetsV2/ios27/AssetData", "platformIdentifier": "com.apple.platform.iphonesimulator",
          "runtimeIdentifier": "com.apple.CoreSimulator.SimRuntime.iOS-27-0", "deletable": true},
  "DDD": {"identifier": "DDD", "version": "18.4", "build": "22E238", "state": "Ready", "sizeBytes": 8200000000,
          "path": "/Library/Developer/CoreSimulator/Cryptex/Images/ios184.dmg", "platformIdentifier": "com.apple.platform.iphonesimulator",
          "runtimeIdentifier": "com.apple.CoreSimulator.SimRuntime.iOS-18-4", "deletable": true},
  "EEE": {"identifier": "EEE", "version": "2.5", "build": "22O473", "state": "Ready", "sizeBytes": 7700000000,
          "path": "/Library/Developer/CoreSimulator/Cryptex/Images/xros25.dmg", "platformIdentifier": "com.apple.platform.xrsimulator",
          "runtimeIdentifier": "com.apple.CoreSimulator.SimRuntime.xrOS-2-5", "deletable": true}
}`

// devicesOnNov8 — iPad для скриншотов, у которого «потерялась» iOS 27.0,
// и запущенный iPhone на iOS 18.4.
const devicesOnNov8 = `{"devices": {
  "com.apple.CoreSimulator.SimRuntime.iOS-27-0": [
    {"udid": "D68E", "name": "iPad Pro 13 store", "state": "Shutdown", "isAvailable": false,
     "availabilityError": "runtime profile not found", "dataPathSize": 5000000000}
  ],
  "com.apple.CoreSimulator.SimRuntime.iOS-18-4": [
    {"udid": "5D48", "name": "iPhone 16", "state": "Booted", "isAvailable": true, "dataPathSize": 3000000000}
  ]
}}`

// fakeSimctl — провайдер, который вместо xcrun отдаёт записанный JSON.
func fakeSimctl(t *testing.T) *Provider {
	t.Helper()
	return &Provider{
		dyldRoot: t.TempDir(),
		run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if slices.Contains(args, "runtime") {
				return []byte(runtimesOnNov8), nil
			}
			return []byte(devicesOnNov8), nil
		},
	}
}

// find — объект по ID.
func find(t *testing.T, items []core.Item, id string) core.Item {
	t.Helper()
	i := slices.IndexFunc(items, func(it core.Item) bool { return it.ID == id })
	if i < 0 {
		t.Fatalf("item %q not found", id)
	}
	return items[i]
}

// whyNot — ключ причины, почему объект нельзя выбрать ("" — можно).
func whyNot(it core.Item) string {
	if ok, why := it.CanSelect(); !ok {
		return why.Key
	}
	return ""
}

func TestScanProtectsWhatWeLostOnNov8(t *testing.T) {
	items, err := fakeSimctl(t).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	providertest.CheckCategories(t, New(), items)

	t.Run("all entries sharing one image are protected, the unusable ones too", func(t *testing.T) {
		for _, id := range []string{"AAA", "BBB", "CCC"} {
			if got := whyNot(find(t, items, id)); got != reasonSharedImage {
				t.Errorf("runtime %s: reason = %q, want %q", id, got, reasonSharedImage)
			}
		}
	})

	t.Run("simulator whose runtime is missing is protected", func(t *testing.T) {
		if got := whyNot(find(t, items, "D68E")); got != reasonRuntimeMissing {
			t.Errorf("iPad reason = %q, want %q", got, reasonRuntimeMissing)
		}
	})

	t.Run("newest runtime of a platform is protected", func(t *testing.T) {
		// visionOS 2.5 — единственная (а значит, самая новая) версия visionOS
		if got := whyNot(find(t, items, "EEE")); got != reasonNewestRuntime {
			t.Errorf("visionOS 2.5 reason = %q, want %q", got, reasonNewestRuntime)
		}
	})

	t.Run("runtime with a booted simulator is running", func(t *testing.T) {
		// iOS 18.4 не самая новая и образ ни с кем не делит — но на ней
		// запущен iPhone 16, поэтому выбрать её нельзя.
		if got := whyNot(find(t, items, "DDD")); got != reasonDeviceBooted {
			t.Errorf("iOS 18.4 reason = %q, want %q", got, reasonDeviceBooted)
		}
		if got := whyNot(find(t, items, "5D48")); got != core.ReasonRunning {
			t.Errorf("iPhone 16 reason = %q, want running", got)
		}
	})

	t.Run("names are readable", func(t *testing.T) {
		if got := find(t, items, "EEE").Name; got != "visionOS 2.5 (22O473)" {
			t.Errorf("name = %q", got)
		}
		if got := find(t, items, "D68E").Name; got != "iPad Pro 13 store · iOS 27.0" {
			t.Errorf("name = %q", got)
		}
	})
}

func TestDyldCache(t *testing.T) {
	// Given: кэш dyld для удалённой iOS 18.4
	p := fakeSimctl(t)
	dir := filepath.Join(p.dyldRoot, "26A434", "com.apple.CoreSimulator.SimRuntime.iOS-18-4.22E238")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dyld_sim_shared_cache_arm64e"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// When
	items, _ := p.Scan(context.Background())
	cache := find(t, items, dir)
	actions, _ := p.Plan([]core.Item{cache})

	// Then: понятное имя и удаление с паролем администратора
	if cache.Name != "iOS 18.4 (22E238)" {
		t.Errorf("name = %q", cache.Name)
	}
	if len(actions) != 1 || !actions[0].NeedsAdmin || actions[0].RemovePath != dir {
		t.Errorf("actions = %+v", actions)
	}
}

func TestPlanUsesSimctl(t *testing.T) {
	p := fakeSimctl(t)
	actions, _ := p.Plan([]core.Item{
		{ID: "DDD", CategoryID: catRuntimes},
		{ID: "5D48", CategoryID: catDevices},
	})

	got := []string{actions[0].String(), actions[1].String()}
	want := []string{"/usr/bin/xcrun simctl runtime delete DDD", "/usr/bin/xcrun simctl delete 5D48"}
	if !slices.Equal(got, want) {
		t.Errorf("plan = %v, want %v", got, want)
	}
	// И ни одной команды «удалить все недоступные».
	for _, a := range actions {
		if strings.Contains(a.String(), "unavailable") {
			t.Errorf("plan must never use 'delete unavailable': %s", a)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("27.0", "18.5") <= 0 || compareVersions("18.10", "18.9") <= 0 || compareVersions("2.5", "2.5") != 0 {
		t.Error("versions must compare as numbers")
	}
}

func TestTranslations(t *testing.T) {
	providertest.CheckTranslations(t, New(),
		reasonSharedImage, reasonNewestRuntime, reasonNotDeletable, reasonRuntimeMissing, reasonNoXcode, reasonDeviceBooted)
}
