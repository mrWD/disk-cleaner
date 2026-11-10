// Package projects is the provider for dependencies and builds in the main
// git repositories: node_modules, Pods, build, .venv, target…
//
// Projects fall into two categories by the date of the last commit:
//   - inactive (not touched for a long time): deleting their dependencies is the safest;
//   - active: also shown, but marked "Caution": after cleaning,
//     you will have to install the dependencies again in a project you are working on.
//
// Only what git ignores is deleted (see the gitscan package).
// Worktrees inside repositories (.claude/worktrees) are not included here:
// they have their own provider.
package projects

import (
	"cmp"
	"context"
	"embed"
	"os"
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
	catBuilds   = "projects.builds" // finished builds that git does not track

	// Reason: a process is running from the project (dev server, Gradle).
	reasonProcessRunning = "projects.process_running"
	reasonNoRoots        = "projects.no_roots" // no project folder exists

	// InactiveAfter: a project with no commits for longer than this counts as inactive.
	InactiveAfter = 30 * 24 * time.Hour
)

// Provider looks for repositories in the given folders.
type Provider struct {
	roots     func() []string // where to look (the "Project folders" setting)
	run       shell.Runner
	processes func(ctx context.Context) []string
	minSize   core.Bytes       // smaller than this is not shown (in tests, 1 byte)
	now       func() time.Time // current time; fixed in tests
}

// New is the provider; roots is called on every scan.
func New(roots func() []string) *Provider {
	return &Provider{roots: roots, run: shell.Run, processes: kit.Processes, now: time.Now, minSize: gitscan.MinSize}
}

func (p *Provider) ID() string { return "projects" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catInactive, Risk: core.RiskSafe},
		{ID: catActive, Risk: core.RiskCaution},
		// An archive may be needed to decode crashes of the version in the App Store.
		{ID: catBuilds, Risk: core.RiskCaution},
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	processes := p.processes(ctx)

	var items []core.Item
	var paths []string

	roots := existing(p.roots())
	if len(roots) == 0 {
		// Not "nothing found": there was nowhere to look, and the user should know.
		return nil, core.Unavailable(reasonNoRoots)
	}

	for _, root := range roots {
		core.ReportLooking(ctx, root) // the window shows where we're looking for repositories
		for _, repo := range gitscan.FindRepos(root) {
			if repo.Kind != gitscan.Main {
				continue // a worktree: it belongs to the worktrees provider
			}

			last := gitscan.LastCommit(ctx, p.run, repo.Path)
			category := catActive
			if last.IsZero() || p.now().Sub(last) > InactiveAfter {
				category = catInactive
			}

			// "Lingary/lingary" is the project path from the root, clearer than just "lingary".
			label, err := filepath.Rel(root, repo.Path)
			if err != nil || label == "." {
				label = filepath.Base(repo.Path)
			}

			detail := kit.Tilde(repo.Path)
			if !last.IsZero() {
				detail += " · " + last.Format("2006-01-02") // date of the last commit
			}

			process := kit.FindProcess(processes, repo.Path+"/")

			// add puts a found folder or file into the right category.
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

			// Dependency and build folders: only the ones git ignores.
			for _, rel := range gitscan.Ignored(ctx, p.run, repo.Path, gitscan.FindArtifacts(repo.Path)) {
				add(category, rel)
			}
			// Finished builds next to the code: only the ones git does not track.
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

// existing — the folders from the list that exist on disk.
func existing(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			out = append(out, d)
		}
	}
	return out
}
