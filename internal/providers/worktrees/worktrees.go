// Package worktrees is the provider for "artifacts" in git worktrees: node_modules,
// Pods, build and other folders that installing and building create.
//
// A worktree is an extra working copy of a repository (git worktree add).
// Claude Code sessions create lots of them: courses/.claude/worktrees/*.
// The code is in git, but dependencies and builds take gigabytes.
//
// Finding repositories and checking "does git ignore this" live in the gitscan
// package, shared with the projects provider (the main repositories).
package worktrees

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
	catArtifacts = "worktrees.artifacts"
	catBuilds    = "worktrees.builds" // finished builds that git does not track

	// Reason: a process is running from the worktree (text is in translations).
	reasonProcessRunning = "worktrees.process_running"
	reasonNoRoots        = "worktrees.no_roots" // no project folder exists
)

// Provider looks for worktrees in the given folders.
type Provider struct {
	roots     func() []string // where to look; a function because the user changes this in settings
	run       shell.Runner
	processes func(ctx context.Context) []string
	minSize   core.Bytes // smaller than this is not shown (in tests, 1 byte)
}

// New is the provider; roots is called on every scan.
func New(roots func() []string) *Provider {
	return &Provider{roots: roots, run: shell.Run, processes: kit.Processes, minSize: gitscan.MinSize}
}

func (p *Provider) ID() string { return "worktrees" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catArtifacts, Risk: core.RiskSafe},
		// An archive may be needed to decode crashes of the version in the App Store.
		{ID: catBuilds, Risk: core.RiskCaution},
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	processes := p.processes(ctx)

	var paths []string
	var items []core.Item

	roots := existing(p.roots())
	if len(roots) == 0 {
		// Not "nothing found": there was nowhere to look, and the user should know.
		return nil, core.Unavailable(reasonNoRoots)
	}

	for _, root := range roots {
		core.ReportLooking(ctx, root) // the window shows where we're looking for repositories
		for _, repo := range gitscan.FindRepos(root) {
			if repo.Kind != gitscan.Worktree {
				continue // main repositories belong to the projects provider
			}
			wt := repo.Path

			branch := ""
			if b := gitscan.Branch(ctx, p.run, wt); b != "" {
				branch = " · " + b
			}
			// A process started from this worktree (Metro, Expo, Vite, Gradle…)
			// holds its node_modules open, so we must not delete under it.
			process := kit.FindProcess(processes, wt+"/")

			// add puts a found folder or file into the right category.
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

			// Dependency and build folders: only the ones git ignores.
			for _, rel := range gitscan.Ignored(ctx, p.run, wt, gitscan.FindArtifacts(wt)) {
				add(catArtifacts, rel)
			}
			// Finished builds next to the code: only the ones git does not track.
			for _, rel := range gitscan.Untracked(ctx, p.run, wt, gitscan.FindBuilds(wt)) {
				add(catBuilds, rel)
			}
		}
	}

	// Sizes of all found folders, in parallel.
	sizes := diskutil.SizeAll(ctx, paths, kit.Workers())
	for i := range items {
		items[i].Size = sizes[i]
	}

	// Hide the small stuff; the biggest on top.
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
