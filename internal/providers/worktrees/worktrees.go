// Package worktrees — провайдер «артефактов» в git worktree: node_modules,
// Pods, build и других папок, которые создаются установкой и сборкой.
//
// Worktree — дополнительная рабочая копия репозитория (git worktree add).
// Их много создают сессии Claude Code: courses/.claude/worktrees/*.
// Код в них лежит в git, а вот зависимости и сборки занимают гигабайты.
//
// Поиск репозиториев и проверка «git игнорирует» — в пакете gitscan,
// общем с провайдером projects (основные репозитории).
package worktrees

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
	catArtifacts = "worktrees.artifacts"
	catBuilds    = "worktrees.builds" // готовые сборки, которые git не отслеживает

	// Причина: из worktree запущен процесс (текст в translations).
	reasonProcessRunning = "worktrees.process_running"
)

// Provider ищет worktree в заданных папках.
type Provider struct {
	roots     func() []string // где искать; функция, потому что пользователь меняет это в настройках
	run       shell.Runner
	processes func(ctx context.Context) []string
	minSize   core.Bytes // меньше — не показываем (в тестах — 1 байт)
}

// New — провайдер; roots вызывается при каждом сканировании.
func New(roots func() []string) *Provider {
	return &Provider{roots: roots, run: shell.Run, processes: kit.Processes, minSize: gitscan.MinSize}
}

func (p *Provider) ID() string { return "worktrees" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catArtifacts, Risk: core.RiskSafe},
		// Архив может понадобиться, чтобы расшифровать крэши версии из App Store.
		{ID: catBuilds, Risk: core.RiskCaution},
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	processes := p.processes(ctx)

	var paths []string
	var items []core.Item

	for _, root := range p.roots() {
		for _, repo := range gitscan.FindRepos(root) {
			if repo.Kind != gitscan.Worktree {
				continue // основные репозитории — у провайдера projects
			}
			wt := repo.Path

			branch := ""
			if b := gitscan.Branch(ctx, p.run, wt); b != "" {
				branch = " · " + b
			}
			// Процесс, запущенный из этого worktree (Metro, Expo, Vite, Gradle…),
			// держит его node_modules — удалять под ним нельзя.
			process := kit.FindProcess(processes, wt+"/")

			// add — добавить найденную папку или файл в нужную категорию.
			add := func(cat, rel string) {
				path := filepath.Join(wt, rel)
				it := core.Item{
					ID:         path,
					CategoryID: cat,
					Name:       filepath.Base(wt) + " · " + rel,
					Detail:     kit.Tilde(wt) + branch,
				}
				if process != "" {
					it.Status = core.StatusInUse
					it.Busy = &core.Reason{Key: reasonProcessRunning, Args: map[string]any{"Process": process}}
				}
				items = append(items, it)
				paths = append(paths, path)
			}

			// Папки зависимостей и сборки — только те, что git игнорирует.
			for _, rel := range gitscan.Ignored(ctx, p.run, wt, gitscan.FindArtifacts(wt)) {
				add(catArtifacts, rel)
			}
			// Готовые сборки рядом с кодом — только те, что git не отслеживает.
			for _, rel := range gitscan.Untracked(ctx, p.run, wt, gitscan.FindBuilds(wt)) {
				add(catBuilds, rel)
			}
		}
	}

	// Размеры всех найденных папок — параллельно.
	sizes := diskutil.SizeAll(ctx, paths, kit.Workers())
	for i := range items {
		items[i].Size = sizes[i]
	}

	// Мелочь не показываем; самое большое — сверху.
	items = slices.DeleteFunc(items, func(it core.Item) bool { return it.Size < p.minSize })
	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })

	return items, ctx.Err()
}

func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	return kit.RemoveActions(items), nil
}
