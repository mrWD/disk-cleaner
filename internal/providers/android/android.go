// Package android is the provider for the Android SDK: emulators (AVDs), the system
// images for them, NDK versions and the Android sources.
package android

import (
	"bufio"
	"context"
	"embed"
	"os"
	"path/filepath"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/kit"
)

//go:embed translations
var translations embed.FS

const (
	catAVDs         = "android.avds"
	catSystemImages = "android.system_images"
	catNDK          = "android.ndk"
	catSources      = "android.sources"

	// Reason: the emulator is using the image (text is in translations).
	reasonUsedByEmulator = "android.used_by_emulator"
)

// Provider looks for Android SDK files in the home folder.
type Provider struct {
	sdk       string                             // ~/Library/Android/sdk
	avdHome   string                             // ~/.android/avd
	processes func(ctx context.Context) []string // list of processes; a fake in tests
}

// New is the provider for the current user.
func New() *Provider {
	home := kit.Home()

	sdk := os.Getenv("ANDROID_HOME") // it may be set from the terminal
	if sdk == "" {
		sdk = filepath.Join(home, "Library", "Android", "sdk") // where Android Studio installs it
	}

	return &Provider{
		sdk:       sdk,
		avdHome:   filepath.Join(home, ".android", "avd"),
		processes: kit.Processes,
	}
}

func (p *Provider) ID() string { return "android" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catAVDs, Risk: core.RiskCaution}, // apps and data inside the emulator are lost
		{ID: catSystemImages, Risk: core.RiskCaution},
		{ID: catNDK, Risk: core.RiskCaution},
		// The sources are only needed to read Android code in the editor; builds work without them.
		{ID: catSources, Risk: core.RiskSafe},
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	processes := p.processes(ctx)

	// --- Emulators: <avdHome>/<name>.avd ---
	var avdDirs []string
	usedImages := map[string]string{} // system image → name of the emulator that uses it
	for _, dir := range kit.Subfolders(p.avdHome) {
		if !strings.HasSuffix(dir, ".avd") {
			continue
		}
		avdDirs = append(avdDirs, dir)
		if image := sysdir(filepath.Join(dir, "config.ini")); image != "" {
			usedImages[image] = avdName(dir)
		}
	}

	avds := kit.PathItems(ctx, catAVDs, avdDirs, avdName)
	for i := range avds { // range by index, so that we can change the elements in place
		name := avds[i].Name
		// An emulator is started as "emulator -avd Name" or "emulator @Name".
		if kit.AnyProcess(processes, "-avd "+name) || kit.AnyProcess(processes, "@"+name) {
			avds[i].Status = core.StatusRunning
		}
	}

	// --- System images: <sdk>/system-images/<api>/<type>/<architecture> ---
	var images []string
	for _, api := range kit.Subfolders(filepath.Join(p.sdk, "system-images")) {
		for _, tag := range kit.Subfolders(api) {
			images = append(images, kit.Subfolders(tag)...)
		}
	}

	imageItems := kit.PathItems(ctx, catSystemImages, images, p.imageName)
	for i := range imageItems {
		rel, _ := filepath.Rel(p.sdk, imageItems[i].ID)
		if user, ok := usedImages[rel]; ok {
			imageItems[i].Status = core.StatusInUse
			imageItems[i].Busy = &core.Reason{Key: reasonUsedByEmulator, Args: map[string]any{"Emulator": user}}
		}
	}

	// --- NDK: <sdk>/ndk/<version> ---
	ndks := kit.PathItems(ctx, catNDK, kit.Subfolders(filepath.Join(p.sdk, "ndk")), func(path string) string {
		return "NDK " + filepath.Base(path)
	})

	// --- Sources: <sdk>/sources/android-35 ---
	sources := kit.PathItems(ctx, catSources, kit.Subfolders(filepath.Join(p.sdk, "sources")), func(path string) string {
		return "Sources " + filepath.Base(path)
	})

	items := append(avds, imageItems...)
	items = append(items, ndks...)
	return append(items, sources...), nil
}

func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	actions := kit.RemoveActions(items)

	// Next to an emulator's Name.avd folder there is a Name.ini file; we delete it too,
	// otherwise Android Studio will show a "broken" emulator.
	for _, it := range items {
		if it.CategoryID != catAVDs {
			continue
		}
		ini := strings.TrimSuffix(it.ID, ".avd") + ".ini"
		if _, err := os.Stat(ini); err == nil {
			actions = append(actions, core.Action{RemovePath: ini})
		}
	}

	return actions, nil
}

// avdName: ".../Lingary_Test.avd" → "Lingary_Test".
func avdName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".avd")
}

// imageName: ".../system-images/android-35/google_apis_playstore/arm64-v8a" →
// "android-35 · google_apis_playstore · arm64-v8a".
func (p *Provider) imageName(path string) string {
	rel, err := filepath.Rel(filepath.Join(p.sdk, "system-images"), path)
	if err != nil {
		return filepath.Base(path)
	}
	return strings.ReplaceAll(rel, string(filepath.Separator), " · ")
}

// sysdir reads from the emulator's config.ini which system image it uses:
// the line "image.sysdir.1=system-images/android-35/google_apis_playstore/arm64-v8a/".
func sysdir(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close() // close the file when the function exits

	// bufio.Scanner reads a file line by line.
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=") // "a=b" → "a", "b", true
		if found && strings.TrimSpace(key) == "image.sysdir.1" {
			return strings.TrimSuffix(strings.TrimSpace(value), "/")
		}
	}
	return ""
}
