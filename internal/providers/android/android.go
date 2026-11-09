// Package android — провайдер Android SDK: эмуляторы (AVD), системные
// образы для них, версии NDK и исходники Android.
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

	// Причина: образ использует эмулятор (текст в translations).
	reasonUsedByEmulator = "android.used_by_emulator"
)

// Provider ищет файлы Android SDK в домашней папке.
type Provider struct {
	sdk       string                             // ~/Library/Android/sdk
	avdHome   string                             // ~/.android/avd
	processes func(ctx context.Context) []string // список процессов; в тестах — фейк
}

// New — провайдер для текущего пользователя.
func New() *Provider {
	home := kit.Home()

	sdk := os.Getenv("ANDROID_HOME") // из терминала может быть задан
	if sdk == "" {
		sdk = filepath.Join(home, "Library", "Android", "sdk") // где его ставит Android Studio
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
		{ID: catAVDs, Risk: core.RiskCaution}, // теряются приложения и данные внутри эмулятора
		{ID: catSystemImages, Risk: core.RiskCaution},
		{ID: catNDK, Risk: core.RiskCaution},
		// Исходники нужны только чтобы читать код Android в редакторе — сборка без них работает.
		{ID: catSources, Risk: core.RiskSafe},
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	processes := p.processes(ctx)

	// --- Эмуляторы: <avdHome>/<имя>.avd ---
	var avdDirs []string
	usedImages := map[string]string{} // системный образ → имя эмулятора, который его использует
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
	for i := range avds { // range по индексу, чтобы менять элементы на месте
		name := avds[i].Name
		// Эмулятор запускают как "emulator -avd Имя" или "emulator @Имя".
		if kit.AnyProcess(processes, "-avd "+name) || kit.AnyProcess(processes, "@"+name) {
			avds[i].Status = core.StatusRunning
		}
	}

	// --- Системные образы: <sdk>/system-images/<api>/<тип>/<архитектура> ---
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

	// --- NDK: <sdk>/ndk/<версия> ---
	ndks := kit.PathItems(ctx, catNDK, kit.Subfolders(filepath.Join(p.sdk, "ndk")), func(path string) string {
		return "NDK " + filepath.Base(path)
	})

	// --- Исходники: <sdk>/sources/android-35 ---
	sources := kit.PathItems(ctx, catSources, kit.Subfolders(filepath.Join(p.sdk, "sources")), func(path string) string {
		return "Sources " + filepath.Base(path)
	})

	items := append(avds, imageItems...)
	items = append(items, ndks...)
	return append(items, sources...), nil
}

func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	actions := kit.RemoveActions(items)

	// У эмулятора рядом с папкой Имя.avd лежит файл Имя.ini — удаляем и его,
	// иначе Android Studio будет показывать «сломанный» эмулятор.
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

// sysdir читает из config.ini эмулятора, какой системный образ он использует:
// строка "image.sysdir.1=system-images/android-35/google_apis_playstore/arm64-v8a/".
func sysdir(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close() // закрыть файл при выходе из функции

	// bufio.Scanner читает файл построчно.
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=") // "a=b" → "a", "b", true
		if found && strings.TrimSpace(key) == "image.sysdir.1" {
			return strings.TrimSuffix(strings.TrimSpace(value), "/")
		}
	}
	return ""
}
