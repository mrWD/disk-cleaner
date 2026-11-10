package ui

import (
	"os"
	"path/filepath"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/settings"
)

// The "Never clean" list: items and folders the user asked the app never to
// touch. They stay visible, but get marked Protected with one of the reasons
// below, so the core's usual rules do the rest: they can't be selected,
// Clean All Safe skips them, and they don't count towards the totals.
//
// Items get onto the list with the Never Clean button in the bottom bar
// (for the selected items) or with Add Folder… in Settings. They come off it
// with Allow cleaning (in an expanded category) or Remove in Settings.

const (
	reasonExcluded       = "reason.excluded"        // the item itself is on the list
	reasonExcludedFolder = "reason.excluded_folder" // a folder on the list contains it, or is inside it
)

// isExclusion — whether a Protected reason is one of ours, not a provider's.
func isExclusion(r *core.Reason) bool {
	return r != nil && (r.Key == reasonExcluded || r.Key == reasonExcludedFolder)
}

// matchExclusion finds the list entry that protects an item.
//
//   - exact: the entry is the item itself (same ID);
//   - otherwise, for paths: the entry is a folder the item is inside, or a
//     folder inside the item. The second case matters as much as the first:
//     deleting ~/Library/…/DerivedData/App-abc would also delete a folder
//     inside it that the user wants to keep.
func matchExclusion(it core.Item, list []settings.Exclusion) (e settings.Exclusion, exact, ok bool) {
	for _, e := range list {
		if e.ID == it.ID {
			return e, true, true
		}
	}
	if !filepath.IsAbs(it.ID) {
		return settings.Exclusion{}, false, false // not a path (a Docker image, a simulator): only exact matches count
	}
	for _, e := range list {
		if filepath.IsAbs(e.ID) && (inside(it.ID, e.ID) || inside(e.ID, it.ID)) {
			return e, false, true
		}
	}
	return settings.Exclusion{}, false, false
}

// inside — whether path lies inside dir. The separator matters: without it,
// ~/Projects/application would count as inside ~/Projects/app.
func inside(path, dir string) bool {
	return strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// markExcluded marks items from the list as Protected and unmarks those no
// longer on it. Items a provider protects for its own reason are left alone.
func (u *App) markExcluded(items []core.Item) {
	list := u.opts.Settings.Excluded()
	for i := range items { // by index, to change the items in place
		it := &items[i]
		if it.Protected != nil && !isExclusion(it.Protected) {
			continue
		}

		e, exact, ok := matchExclusion(*it, list)
		switch {
		case !ok:
			it.Protected = nil
		case exact:
			it.Protected = &core.Reason{Key: reasonExcluded}
		default:
			it.Protected = &core.Reason{Key: reasonExcludedFolder, Args: map[string]any{"Folder": e.Detail}}
		}
	}
}

// applyExclusions re-marks everything after the list changed. Items that
// became protected are taken out of the selection: they can't be cleaned.
func (u *App) applyExclusions() {
	for _, st := range u.states {
		u.markExcluded(st.items)
		for _, it := range st.items {
			if it.Protected != nil {
				u.sel.Remove(it)
			}
		}
	}
	u.refresh()
}

// neverClean — the Never Clean button: puts the selected items on the list.
func (u *App) neverClean() {
	list := u.opts.Settings.Excluded()
	for _, it := range u.sel.Items() {
		if _, exact, _ := matchExclusion(it, list); !exact {
			list = append(list, settings.Exclusion{ID: it.ID, Name: it.Name, Detail: it.Detail})
		}
	}
	u.opts.Settings.SetExcluded(list)
	u.sel.Clear()
	u.applyExclusions()
}

// allowCleaning — the Allow cleaning link: takes the item off the list.
func (u *App) allowCleaning(it core.Item) {
	var list []settings.Exclusion
	for _, e := range u.opts.Settings.Excluded() {
		if e.ID != it.ID {
			list = append(list, e)
		}
	}
	u.opts.Settings.SetExcluded(list)
	u.applyExclusions()
}

// tildePath shows a path inside the home folder as ~/…, like Finder and Terminal.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == home {
		return "~"
	}
	if inside(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
