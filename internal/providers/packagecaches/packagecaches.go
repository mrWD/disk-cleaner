// Package packagecaches — провайдер кэшей менеджеров пакетов и
// инструментов разработки: npm, Yarn, CocoaPods, Gradle, uv, pip,
// Homebrew, Go, Swift Package Manager, браузеры Puppeteer и Playwright.
//
// Всё это — скачанные копии того, что лежит в интернете. Удаление
// безопасно: инструмент скачает нужное снова при следующей установке.
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

// Причина: работает демон Gradle (текст в translations).
const reasonGradleDaemon = "packagecaches.gradle_daemon"

// folder — одна папка кэша: как её назвать и где она (путь от домашней папки).
// В пути можно писать * (как в терминале): ".gradle/jdks/*.tar.gz" —
// тогда каждый найденный файл станет отдельной строкой.
type folder struct {
	name string
	path string
}

// cache — одна категория: ID, риск и папки, которые в неё входят.
type cache struct {
	id      string
	risk    core.Risk
	folders []folder
}

// caches — всё, что умеет находить провайдер. Добавить новый кэш —
// это одна запись здесь и четыре текста в translations/*.json.
var caches = []cache{
	{"packagecaches.npm", core.RiskSafe, []folder{
		{"npm", ".npm/_cacache"},
		{"npx cache", ".npm/_npx"}, // пакеты, скачанные командой npx
	}},
	{"packagecaches.yarn", core.RiskSafe, []folder{{"Yarn", "Library/Caches/Yarn"}}},
	{"packagecaches.cocoapods", core.RiskSafe, []folder{{"CocoaPods", "Library/Caches/CocoaPods"}}},
	{"packagecaches.gradle", core.RiskSafe, []folder{
		{"Gradle caches", ".gradle/caches"},
		{"Gradle wrapper distributions", ".gradle/wrapper/dists"},
		// Архивы, из которых Gradle распаковал JDK. Сами распакованные JDK
		// не трогаем: без них сборка может не найти нужную версию Java.
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

// Provider ищет кэши в домашней папке.
type Provider struct {
	home      string
	processes func(ctx context.Context) []string // список процессов; в тестах — фейк
}

// New — провайдер для текущего пользователя.
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
	// Работающий демон Gradle держит открытыми файлы в своём кэше:
	// удалять кэш под ним — сломать идущую сборку.
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

// expand — пути одной записи таблицы: сама папка или, если в пути есть *,
// все подходящие файлы. Ключ — имя для показа: "Gradle JDK download · OpenJDK17U….tar.gz".
func (p *Provider) expand(f folder) map[string]string {
	full := filepath.Join(p.home, f.path)
	if !strings.ContainsAny(f.path, "*?[") {
		return map[string]string{f.name: full}
	}

	matches, _ := filepath.Glob(full) // ошибка бывает только у неправильного шаблона
	out := map[string]string{}
	for _, m := range matches {
		out[f.name+" · "+filepath.Base(m)] = m
	}
	return out
}

// Plan: удалить папки. Кэш модулей Go — только для чтения; исполнитель
// умеет снять эту защиту перед удалением.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	return kit.RemoveActions(items), nil
}
