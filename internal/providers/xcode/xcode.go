// Package xcode is the provider for Xcode files: builds (DerivedData), archives,
// device support symbols, the documentation cache, SwiftUI previews.
//
// Simulators are a separate provider (simulators): they have their own protection rules.
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

// //go:embed is an instruction to the compiler: bake the translations folder into the program.
// The comment must sit right above a variable of type embed.FS.
//
//go:embed translations
var translations embed.FS

// Category IDs. They start with the provider ID; providertest checks that.
const (
	catDerivedData   = "xcode.derived_data"
	catArchives      = "xcode.archives"
	catDeviceSupport = "xcode.device_support"
	catDocumentation = "xcode.documentation"
	catPreviews      = "xcode.previews"
)

// Provider looks for Xcode files in the home folder.
type Provider struct {
	home string // a field, not a call to os.UserHomeDir(): in tests we substitute a temp folder
}

// New is the provider for the current user.
func New() *Provider {
	return &Provider{home: kit.Home()}
}

func (p *Provider) ID() string { return "xcode" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catDerivedData, Risk: core.RiskSafe},
		{ID: catArchives, Risk: core.RiskCaution}, // needed to decode crashes
		{ID: catDeviceSupport, Risk: core.RiskSafe},
		{ID: catDocumentation, Risk: core.RiskSafe},
		{ID: catPreviews, Risk: core.RiskSafe},
	}
}

// xcodeDir is a path inside ~/Library/Developer/Xcode.
func (p *Provider) xcodeDir(parts ...string) string {
	// append([]string{...}, parts...) joins two slices into one.
	return filepath.Join(append([]string{p.home, "Library", "Developer", "Xcode"}, parts...)...)
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	var items []core.Item

	// DerivedData: one item per project.
	derived := kit.Subfolders(p.xcodeDir("DerivedData"))
	items = append(items, kit.PathItems(ctx, catDerivedData, derived, projectName)...)

	// Archives: ~/…/Archives/<date>/<name>.xcarchive
	var archives []string
	for _, day := range kit.Subfolders(p.xcodeDir("Archives")) {
		for _, a := range kit.Subfolders(day) {
			if strings.HasSuffix(a, ".xcarchive") {
				archives = append(archives, a)
			}
		}
	}
	items = append(items, kit.PathItems(ctx, catArchives, archives, archiveName)...)

	// Device support symbols: "iOS DeviceSupport/<model> <version> (<build>)".
	var support []string
	for _, platform := range []string{"iOS", "watchOS", "tvOS", "visionOS", "xrOS", "macOS"} {
		support = append(support, kit.Subfolders(p.xcodeDir(platform+" DeviceSupport"))...)
	}
	items = append(items, kit.PathItems(ctx, catDeviceSupport, support, deviceSupportName)...)

	// Documentation: two folders, each is one item.
	docs := []string{p.xcodeDir("DocumentationCache"), p.xcodeDir("DocumentationIndex")}
	items = append(items, kit.PathItems(ctx, catDocumentation, docs, kit.BaseName)...)

	// SwiftUI previews.
	if it, ok := kit.FolderItem(ctx, catPreviews, "Previews", p.xcodeDir("UserData", "Previews")); ok {
		items = append(items, it)
	}

	// There is no error case here: no folder means there is nothing to show.
	return items, nil
}

// Plan: every item is a folder, and that folder is what we delete.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	return kit.RemoveActions(items), nil
}

// derivedSuffix is the tail Xcode adds to a project name:
// "Lingary-bxqjcmhlwrsvdyfkqlfxonbsnjcn". regexp.MustCompile compiles
// the regular expression once, when the program starts.
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
