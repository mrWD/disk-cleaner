// Package simulators — провайдер симуляторов Xcode: версии систем
// (runtimes), сами симуляторы (devices) и кэш dyld.
//
// Здесь живут правила, выученные на ошибках 08.11.2026:
//   - Несколько записей runtime могут указывать на ОДИН образ на диске
//     (так было с тремя записями iOS 27.0). Удалишь «лишнюю» — пропадёт
//     и рабочая. Такие записи защищены.
//   - Симулятор, у которого временно «потерялась» версия системы, выглядит
//     недоступным, и xcrun simctl delete unavailable удаляет его вместе
//     с данными. Здесь такие симуляторы защищены.
//   - Самую новую версию каждой платформы тоже не даём удалить:
//     без неё Xcode не сможет запустить ни одного симулятора.
package simulators

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/shell"
)

//go:embed translations
var translations embed.FS

const (
	catRuntimes = "simulators.runtimes"
	catDevices  = "simulators.devices"
	catDyld     = "simulators.dyld_cache"

	// Причины защиты и недоступности (тексты в translations).
	reasonSharedImage    = "simulators.shared_image"
	reasonNewestRuntime  = "simulators.newest_runtime"
	reasonNotDeletable   = "simulators.not_deletable"
	reasonRuntimeMissing = "simulators.runtime_missing"
	reasonNoXcode        = "simulators.no_xcode"
	reasonDeviceBooted   = "simulators.device_booted"

	xcrun = "/usr/bin/xcrun"
)

// Provider спрашивает симуляторы через xcrun simctl.
type Provider struct {
	run      shell.Runner // запуск xcrun; в тестах — фейк с записанным JSON
	dyldRoot string       // где лежит кэш dyld; в тестах — временная папка
}

// New — провайдер для этого компьютера.
func New() *Provider {
	return &Provider{run: shell.Run, dyldRoot: "/Library/Developer/CoreSimulator/Caches/dyld"}
}

func (p *Provider) ID() string { return "simulators" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catRuntimes, Risk: core.RiskCaution}, // скачивать заново ~8 ГБ
		{ID: catDevices, Risk: core.RiskCaution},  // теряются установленные приложения и их данные
		{ID: catDyld, Risk: core.RiskSafe},
	}
}

// runtimeJSON — одна запись из xcrun simctl runtime list -j.
// Теги `json:"..."` говорят, как поле называется в JSON.
type runtimeJSON struct {
	Identifier         string `json:"identifier"`
	Version            string `json:"version"`
	Build              string `json:"build"`
	State              string `json:"state"`
	SizeBytes          int64  `json:"sizeBytes"`
	Path               string `json:"path"` // путь к образу на диске
	PlatformIdentifier string `json:"platformIdentifier"`
	RuntimeIdentifier  string `json:"runtimeIdentifier"`
	Deletable          bool   `json:"deletable"`
}

// deviceJSON — один симулятор из xcrun simctl list devices -j.
type deviceJSON struct {
	UDID              string `json:"udid"`
	Name              string `json:"name"`
	State             string `json:"state"` // "Booted", "Shutdown"
	IsAvailable       bool   `json:"isAvailable"`
	AvailabilityError string `json:"availabilityError"`
	DataPath          string `json:"dataPath"`
	DataPathSize      int64  `json:"dataPathSize"`
	LastBootedAt      string `json:"lastBootedAt"`
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	runtimesOut, err := p.run(ctx, xcrun, "simctl", "runtime", "list", "-j")
	if err != nil {
		return nil, core.Unavailable(reasonNoXcode)
	}
	devicesOut, err := p.run(ctx, xcrun, "simctl", "list", "devices", "-j")
	if err != nil {
		return nil, core.Unavailable(reasonNoXcode)
	}

	// runtime list -j — словарь «ID → запись».
	var runtimes map[string]runtimeJSON
	if err := json.Unmarshal(runtimesOut, &runtimes); err != nil {
		return nil, fmt.Errorf("simctl runtime list: %w", err)
	}

	// list devices -j — {"devices": {"<ID версии>": [симуляторы…]}}.
	var devices struct {
		Devices map[string][]deviceJSON `json:"devices"`
	}
	if err := json.Unmarshal(devicesOut, &devices); err != nil {
		return nil, fmt.Errorf("simctl list devices: %w", err)
	}

	items := runtimeItems(runtimes, devices.Devices)
	items = append(items, p.deviceItems(ctx, devices.Devices)...)
	items = append(items, p.dyldItems(ctx)...)

	return items, nil
}

// runtimeItems — объекты для версий систем и их защита.
func runtimeItems(runtimes map[string]runtimeJSON, devices map[string][]deviceJSON) []core.Item {
	// Сколько записей указывают на каждый образ на диске.
	byImage := map[string]int{}
	for _, r := range runtimes {
		byImage[r.Path]++
	}

	// Самая новая версия каждой платформы.
	newest := map[string]string{} // платформа → версия
	for _, r := range runtimes {
		if cur, ok := newest[r.PlatformIdentifier]; !ok || compareVersions(r.Version, cur) > 0 {
			newest[r.PlatformIdentifier] = r.Version
		}
	}

	var items []core.Item
	for _, r := range runtimes {
		name := runtimeName(r)
		it := core.Item{
			ID:         r.Identifier,
			CategoryID: catRuntimes,
			Name:       name,
			Detail:     r.State,
			Size:       core.Bytes(r.SizeBytes),
		}

		// Запущен ли симулятор на этой версии.
		for _, d := range devices[r.RuntimeIdentifier] {
			if d.State == "Booted" {
				it.Status = core.StatusRunning
				it.Busy = &core.Reason{Key: reasonDeviceBooted, Args: map[string]any{"Device": d.Name}}
			}
		}

		// Защита — от самой важной причины к менее важной.
		switch {
		case byImage[r.Path] > 1:
			it.Protected = &core.Reason{Key: reasonSharedImage, Args: map[string]any{"Count": byImage[r.Path]}}
		case !r.Deletable:
			it.Protected = &core.Reason{Key: reasonNotDeletable}
		case newest[r.PlatformIdentifier] == r.Version:
			it.Protected = &core.Reason{Key: reasonNewestRuntime, Args: map[string]any{"Runtime": name}}
		}

		items = append(items, it)
	}

	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(a.Name, b.Name) })
	return items
}

// deviceItems — объекты для симуляторов.
func (p *Provider) deviceItems(ctx context.Context, devices map[string][]deviceJSON) []core.Item {
	var items []core.Item

	for runtimeID, list := range devices {
		for _, d := range list {
			size := core.Bytes(d.DataPathSize)
			if size == 0 && d.DataPath != "" {
				// Старый Xcode не отдаёт размер — считаем сами
				// (папка симулятора — на уровень выше data).
				size, _ = diskutil.DirSize(ctx, filepath.Dir(d.DataPath))
			}

			it := core.Item{
				ID:         d.UDID,
				CategoryID: catDevices,
				Name:       d.Name + " · " + runtimeNameFromID(runtimeID),
				Detail:     d.State,
				Size:       size,
			}
			if d.State == "Booted" {
				it.Status = core.StatusRunning
			}
			if !d.IsAvailable {
				// Тот самый случай: версия системы временно не найдена.
				// Удалять такой симулятор — терять его данные навсегда.
				it.Protected = &core.Reason{Key: reasonRuntimeMissing}
			}
			items = append(items, it)
		}
	}

	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(a.Name, b.Name) })
	return items
}

// dyldItems — кэш dyld: <dyldRoot>/<сборка macOS>/<версия симулятора>.
func (p *Provider) dyldItems(ctx context.Context) []core.Item {
	var paths []string
	for _, host := range kit.Subfolders(p.dyldRoot) {
		paths = append(paths, kit.Subfolders(host)...)
	}

	return kit.PathItems(ctx, catDyld, paths, func(path string) string {
		return dyldName(filepath.Base(path))
	})
}

func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	var actions []core.Action

	for _, it := range items {
		switch it.CategoryID {
		case catRuntimes:
			actions = append(actions, core.Action{Command: []string{xcrun, "simctl", "runtime", "delete", it.ID}})
		case catDevices:
			actions = append(actions, core.Action{Command: []string{xcrun, "simctl", "delete", it.ID}})
		case catDyld:
			// Кэш dyld принадлежит root — нужен пароль администратора.
			actions = append(actions, core.Action{RemovePath: it.ID, NeedsAdmin: true})
		}
	}

	return actions, nil
}

// platformNames — человеческие имена платформ.
var platformNames = map[string]string{
	"com.apple.platform.iphonesimulator":  "iOS",
	"com.apple.platform.appletvsimulator": "tvOS",
	"com.apple.platform.watchsimulator":   "watchOS",
	"com.apple.platform.xrsimulator":      "visionOS",
}

// runtimeName: "iOS 27.0 (24A434)".
func runtimeName(r runtimeJSON) string {
	platform := platformNames[r.PlatformIdentifier]
	if platform == "" {
		platform = r.PlatformIdentifier
	}
	return fmt.Sprintf("%s %s (%s)", platform, r.Version, r.Build)
}

// runtimeIDRe разбирает "com.apple.CoreSimulator.SimRuntime.iOS-18-4".
var runtimeIDRe = regexp.MustCompile(`SimRuntime\.([A-Za-z]+)-([0-9-]+)`)

// runtimeNameFromID: "com.apple.CoreSimulator.SimRuntime.iOS-18-4" → "iOS 18.4".
func runtimeNameFromID(id string) string {
	m := runtimeIDRe.FindStringSubmatch(id)
	if m == nil {
		return id
	}
	platform := m[1]
	if platform == "xrOS" {
		platform = "visionOS"
	}
	return platform + " " + strings.ReplaceAll(m[2], "-", ".")
}

// dyldName: "com.apple.CoreSimulator.SimRuntime.iOS-18-4.22E238" → "iOS 18.4 (22E238)".
func dyldName(dir string) string {
	build := dir[strings.LastIndex(dir, ".")+1:]
	return runtimeNameFromID(dir) + " (" + build + ")"
}

// compareVersions сравнивает "27.0" и "18.5" как числа: 27 > 18.
// Возвращает -1, 0 или 1, как cmp.Compare.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(as), len(bs)) {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if c := cmp.Compare(x, y); c != 0 {
			return c
		}
	}
	return 0
}
