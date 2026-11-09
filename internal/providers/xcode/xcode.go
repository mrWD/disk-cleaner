// Package xcode — провайдер файлов Xcode: сборки (DerivedData), архивы,
// символы устройств, кэш документации, превью SwiftUI.
//
// Симуляторы — отдельный провайдер (simulators): у них свои правила защиты.
package xcode

import (
	"context"
	"embed"
	"path/filepath"
	"regexp"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/kit"
)

// //go:embed — указание компилятору: вшить папку translations в программу.
// Комментарий должен стоять прямо над переменной типа embed.FS.
//
//go:embed translations
var translations embed.FS

// ID категорий. Начинаются с ID провайдера — это проверяет providertest.
const (
	catDerivedData   = "xcode.derived_data"
	catArchives      = "xcode.archives"
	catDeviceSupport = "xcode.device_support"
	catDocumentation = "xcode.documentation"
	catPreviews      = "xcode.previews"
)

// Provider ищет файлы Xcode в домашней папке.
type Provider struct {
	home string // поле, а не вызов os.UserHomeDir(): в тестах подставим временную папку
}

// New — провайдер для текущего пользователя.
func New() *Provider {
	return &Provider{home: kit.Home()}
}

func (p *Provider) ID() string { return "xcode" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catDerivedData, Risk: core.RiskSafe},
		{ID: catArchives, Risk: core.RiskCaution}, // нужны для расшифровки крэшей
		{ID: catDeviceSupport, Risk: core.RiskSafe},
		{ID: catDocumentation, Risk: core.RiskSafe},
		{ID: catPreviews, Risk: core.RiskSafe},
	}
}

// xcodeDir — путь внутри ~/Library/Developer/Xcode.
func (p *Provider) xcodeDir(parts ...string) string {
	// append([]string{...}, parts...) — склеить два среза в один.
	return filepath.Join(append([]string{p.home, "Library", "Developer", "Xcode"}, parts...)...)
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	var items []core.Item

	// DerivedData: по объекту на проект.
	derived := kit.Subfolders(p.xcodeDir("DerivedData"))
	items = append(items, kit.PathItems(ctx, catDerivedData, derived, projectName)...)

	// Archives: ~/…/Archives/<дата>/<имя>.xcarchive
	var archives []string
	for _, day := range kit.Subfolders(p.xcodeDir("Archives")) {
		for _, a := range kit.Subfolders(day) {
			if strings.HasSuffix(a, ".xcarchive") {
				archives = append(archives, a)
			}
		}
	}
	items = append(items, kit.PathItems(ctx, catArchives, archives, archiveName)...)

	// Символы устройств: "iOS DeviceSupport/<модель> <версия> (<сборка>)".
	var support []string
	for _, platform := range []string{"iOS", "watchOS", "tvOS", "visionOS", "xrOS", "macOS"} {
		support = append(support, kit.Subfolders(p.xcodeDir(platform+" DeviceSupport"))...)
	}
	items = append(items, kit.PathItems(ctx, catDeviceSupport, support, deviceSupportName)...)

	// Документация: две папки, каждая — один объект.
	docs := []string{p.xcodeDir("DocumentationCache"), p.xcodeDir("DocumentationIndex")}
	items = append(items, kit.PathItems(ctx, catDocumentation, docs, kit.BaseName)...)

	// Превью SwiftUI.
	if it, ok := kit.FolderItem(ctx, catPreviews, "Previews", p.xcodeDir("UserData", "Previews")); ok {
		items = append(items, it)
	}

	// Ошибки здесь не бывает: нет папки — значит, нечего показать.
	return items, nil
}

// Plan: каждый объект — папка, её и удаляем.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	return kit.RemoveActions(items), nil
}

// derivedSuffix — хвост, который Xcode добавляет к имени проекта:
// "Lingary-bxqjcmhlwrsvdyfkqlfxonbsnjcn". regexp.MustCompile компилирует
// регулярное выражение один раз при запуске программы.
var derivedSuffix = regexp.MustCompile(`-[a-z]{28}$`)

// projectName: "Lingary-bxqj…" → "Lingary".
func projectName(path string) string {
	return derivedSuffix.ReplaceAllString(filepath.Base(path), "")
}

// archiveName: "…/2026-11-08/ITSkills 08.11.2026, 14.20.xcarchive" →
// "ITSkills 08.11.2026, 14.20".
func archiveName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".xcarchive")
}

// deviceSupportName: "…/iOS DeviceSupport/iPhone17,1 26.0 (23A341)" →
// "iOS · iPhone17,1 26.0 (23A341)".
func deviceSupportName(path string) string {
	platform := strings.TrimSuffix(filepath.Base(filepath.Dir(path)), " DeviceSupport")
	return platform + " · " + filepath.Base(path)
}
