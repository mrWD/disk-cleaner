// Package projects — провайдер зависимостей и сборок в основных
// git-репозиториях: node_modules, Pods, build, .venv, target…
//
// Проекты делятся на две категории по дате последнего коммита:
//   - неактивные (давно не трогали) — их зависимости удалять безопаснее всего;
//   - активные — тоже показываются, но с меткой «Caution»: после очистки
//     придётся заново ставить зависимости в проекте, с которым работаешь.
//
// Удаляется только то, что git игнорирует (см. пакет gitscan).
// Worktree внутри репозиториев (.claude/worktrees) сюда не попадают —
// у них свой провайдер.
package projects

import (
	"cmp"
	"context"
	"embed"
	"path/filepath"
	"slices"
	"time"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
	"disk-cleaner/internal/providers/gitscan"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/shell"
)

//go:embed translations
var translations embed.FS

const (
	catInactive = "projects.inactive"
	catActive   = "projects.active"
	catBuilds   = "projects.builds" // готовые сборки, которые git не отслеживает

	// Причина: из проекта запущен процесс (dev-сервер, Gradle).
	reasonProcessRunning = "projects.process_running"

	// InactiveAfter — проект без коммитов дольше этого срока считается неактивным.
	InactiveAfter = 30 * 24 * time.Hour
)

// Provider ищет репозитории в заданных папках.
type Provider struct {
	roots     func() []string // где искать (настройка «Папки с проектами»)
	run       shell.Runner
	processes func(ctx context.Context) []string
	minSize   core.Bytes       // меньше — не показываем (в тестах — 1 байт)
	now       func() time.Time // текущее время; в тестах — фиксированное
}

// New — провайдер; roots вызывается при каждом сканировании.
func New(roots func() []string) *Provider {
	return &Provider{roots: roots, run: shell.Run, processes: kit.Processes, now: time.Now, minSize: gitscan.MinSize}
}

func (p *Provider) ID() string { return "projects" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catInactive, Risk: core.RiskSafe},
		{ID: catActive, Risk: core.RiskCaution},
		// Архив может понадобиться, чтобы расшифровать крэши версии из App Store.
		{ID: catBuilds, Risk: core.RiskCaution},
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	processes := p.processes(ctx)

	var items []core.Item
	var paths []string

	for _, root := range p.roots() {
		for _, repo := range gitscan.FindRepos(root) {
			if repo.Kind != gitscan.Main {
				continue // worktree — у провайдера worktrees
			}

			last := gitscan.LastCommit(ctx, p.run, repo.Path)
			category := catActive
			if last.IsZero() || p.now().Sub(last) > InactiveAfter {
				category = catInactive
			}

			// "Lingary/lingary" — путь проекта от корня, понятнее, чем просто "lingary".
			label, err := filepath.Rel(root, repo.Path)
			if err != nil || label == "." {
				label = filepath.Base(repo.Path)
			}

			detail := kit.Tilde(repo.Path)
			if !last.IsZero() {
				detail += " · " + last.Format("2006-01-02") // дата последнего коммита
			}

			process := kit.FindProcess(processes, repo.Path+"/")

			// add — добавить найденную папку или файл в нужную категорию.
			add := func(cat, rel string) {
				path := filepath.Join(repo.Path, rel)
				it := core.Item{
					ID:         path,
					CategoryID: cat,
					Name:       label + " · " + rel,
					Detail:     detail,
				}
				if process != "" {
					it.Status = core.StatusInUse
					it.Busy = &core.Reason{Key: reasonProcessRunning, Args: map[string]any{"Process": process}}
				}
				items = append(items, it)
				paths = append(paths, path)
			}

			// Папки зависимостей и сборки — только те, что git игнорирует.
			for _, rel := range gitscan.Ignored(ctx, p.run, repo.Path, gitscan.FindArtifacts(repo.Path)) {
				add(category, rel)
			}
			// Готовые сборки рядом с кодом — только те, что git не отслеживает.
			for _, rel := range gitscan.Untracked(ctx, p.run, repo.Path, gitscan.FindBuilds(repo.Path)) {
				add(catBuilds, rel)
			}
		}
	}

	sizes := diskutil.SizeAll(ctx, paths, kit.Workers())
	for i := range items {
		items[i].Size = sizes[i]
	}

	items = slices.DeleteFunc(items, func(it core.Item) bool { return it.Size < p.minSize })
	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })

	return items, ctx.Err()
}

func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	return kit.RemoveActions(items), nil
}
