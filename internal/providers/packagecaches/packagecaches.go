// Package packagecaches is the provider for caches of package managers and
// development tools: npm, Yarn, CocoaPods, Gradle, uv, pip,
// Homebrew, Go, Swift Package Manager, Puppeteer and Playwright browsers.
//
// All of this is downloaded copies of what lives on the internet. Deleting it
// is safe: the tool will download what it needs again on the next install.
package packagecaches

import (
	"context"
	"embed"
	"path/filepath"
	"slices"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/providers/kit"
)

//go:embed translations
var translations embed.FS

// Reason: a Gradle daemon is running (text is in translations).
const reasonGradleDaemon = "packagecaches.gradle_daemon"

// folder is one cache folder: what to call it and where it is (path from the home folder).
// The path may contain * (like in the terminal): ".gradle/jdks/*.tar.gz",
// and then each found file becomes a separate row.
type folder struct {
	name string
	path string
}

// cache is one category: ID, risk and the folders it includes.
type cache struct {
	id      string
	risk    core.Risk
	folders []folder
}

// caches is everything the provider can find. Adding a new cache is
// one entry here and four texts in translations/*.json.
var caches = []cache{
	{"packagecaches.npm", core.RiskSafe, []folder{
		{"npm", ".npm/_cacache"},
		{"npx cache", ".npm/_npx"}, // packages downloaded by the npx command
	}},
	{"packagecaches.yarn", core.RiskSafe, []folder{{"Yarn", "Library/Caches/Yarn"}}},
	{"packagecaches.cocoapods", core.RiskSafe, []folder{{"CocoaPods", "Library/Caches/CocoaPods"}}},
	{"packagecaches.gradle", core.RiskSafe, []folder{
		{"Gradle caches", ".gradle/caches"},
		{"Gradle wrapper distributions", ".gradle/wrapper/dists"},
		// Archives that Gradle unpacked JDKs from. We do not touch the unpacked JDKs
		// themselves: without them a build may not find the right Java version.
		{"Gradle JDK download", ".gradle/jdks/*.tar.gz"},
		{"Gradle JDK download", ".gradle/jdks/*.zip"},
	}},
	{"packagecaches.uv", core.RiskSafe, []folder{{"uv", ".cache/uv"}}},
	{"packagecaches.pip", core.RiskSafe, []folder{{"pip", "Library/Caches/pip"}}},
	{"packagecaches.homebrew", core.RiskSafe, []folder{{"Homebrew downloads", "Library/Caches/Homebrew"}}},
	{"packagecaches.go", core.RiskSafe, []folder{
		{"Go build cache", "Library/Caches/go-build"},
		{"Go module cache", "go/pkg/mod"},
	}},
	{"packagecaches.swiftpm", core.RiskSafe, []folder{{"Swift Package Manager", "Library/Caches/org.swift.swiftpm"}}},
	{"packagecaches.browsers", core.RiskCaution, []folder{
		{"Puppeteer browsers", ".cache/puppeteer"},
		{"Playwright browsers", "Library/Caches/ms-playwright"},
		{"Playwright MCP browsers", "Library/Caches/ms-playwright-mcp"},
	}},
	{"packagecaches.electron", core.RiskSafe, []folder{
		{"Electron downloads", "Library/Caches/electron"},
		{"electron-builder downloads", "Library/Caches/electron-builder"},
	}},
	{"packagecaches.expo", core.RiskSafe, []folder{
		{"Expo builds for the simulator", ".expo/ios-simulator-app-cache"},
		{"Expo Go downloads", ".expo/expo-go"},
		{"Expo Android APKs", ".expo/android-apk-cache"},
	}},
	{"packagecaches.jstools", core.RiskSafe, []folder{
		{"React Native prebuilt core", "Library/Caches/ReactNative"},
		{"TypeScript type definitions", "Library/Caches/typescript"},
		{"pnpm metadata", "Library/Caches/pnpm"},
		{"node-gyp headers", "Library/Caches/node-gyp"},
		{"dotslash downloads", "Library/Caches/dotslash"},
		{"Prisma engines", ".cache/prisma"},
		{"Corepack", ".cache/node/corepack"},
	}},
	{"packagecaches.precommit", core.RiskSafe, []folder{{"pre-commit environments", ".cache/pre-commit"}}},
	{"packagecaches.nuget", core.RiskSafe, []folder{{"NuGet", ".nuget/packages"}}},
	{"packagecaches.python", core.RiskSafe, []folder{{"Python bytecode", "Library/Caches/com.apple.python"}}},
}

// Provider looks for caches in the home folder.
type Provider struct {
	home      string
	processes func(ctx context.Context) []string // list of processes; a fake in tests
}

// New is the provider for the current user.
func New() *Provider {
	return &Provider{home: kit.Home(), processes: kit.Processes}
}

func (p *Provider) ID() string { return "packagecaches" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	out := make([]core.Category, 0, len(caches))
	for _, c := range caches {
		out = append(out, core.Category{ID: c.id, Risk: c.risk})
	}
	return out
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	// A running Gradle daemon keeps files in its cache open:
	// deleting the cache under it would break a running build.
	gradleRunning := kit.AnyProcess(p.processes(ctx), "GradleDaemon")

	var items []core.Item
	for _, c := range caches {
		for _, f := range c.folders {
			for name, path := range p.expand(f) {
				it, ok := kit.FolderItem(ctx, c.id, name, path)
				if !ok {
					continue
				}
				if c.id == "packagecaches.gradle" && gradleRunning {
					it.Status = core.StatusRunning
					it.Busy = &core.Reason{Key: reasonGradleDaemon}
				}
				items = append(items, it)
			}
		}
	}
	slices.SortStableFunc(items, func(a, b core.Item) int { return strings.Compare(a.CategoryID, b.CategoryID) })

	return items, nil
}

// expand gives the paths of one table entry: the folder itself or, if the path has *,
// all matching files. The key is the display name: "Gradle JDK download · OpenJDK17U….tar.gz".
func (p *Provider) expand(f folder) map[string]string {
	full := filepath.Join(p.home, f.path)
	if !strings.ContainsAny(f.path, "*?[") {
		return map[string]string{f.name: full}
	}

	matches, _ := filepath.Glob(full) // an error only happens with a malformed pattern
	out := map[string]string{}
	for _, m := range matches {
		out[f.name+" · "+filepath.Base(m)] = m
	}
	return out
}

// Plan: delete the folders. The Go module cache is read-only; the executor
// knows how to remove that protection before deleting.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	return kit.RemoveActions(items), nil
}
