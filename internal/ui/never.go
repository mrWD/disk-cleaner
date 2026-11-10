package ui

import (
	_ "embed" // for the //go:embed lines below
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/settings"
)

// The "Never clean" list: items and folders the user asked the app never to
// touch. They stay visible, but get marked Protected with one of the reasons
// below, so the core's usual rules do the rest: they can't be selected,
// Clean All Safe skips them, and they don't count towards the totals.
//
// Items get onto the list with the lock at the end of their row, with the
// Never Clean button in the bottom bar (for the selected items) or with
// Add Folder… in Settings. They come off it with the same lock, Allow
// cleaning (in an expanded category) or Remove in Settings.
//
// The lock matters for items that are running or in use: they can't be
// ticked, so the bottom-bar button can't reach them, but the lock can.

// The lock icons: closed (on the list) and open (not on it). They are drawn
// with fills only, because Fyne recolours fills but not strokes.
var (
	//go:embed assets/lock.svg
	lockSVG []byte
	//go:embed assets/lock-open.svg
	lockOpenSVG []byte

	lockClosed = fyne.NewStaticResource("lock.svg", lockSVG)
	lockOpen   = fyne.NewStaticResource("lock-open.svg", lockOpenSVG)
)

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
	items := u.sel.Items()
	u.sel.Clear()
	u.addExclusions(items...)
}

// addExclusions puts items on the list (those already there are skipped).
func (u *App) addExclusions(items ...core.Item) {
	list := u.opts.Settings.Excluded()
	for _, it := range items {
		if _, exact, _ := matchExclusion(it, list); !exact {
			list = append(list, settings.Exclusion{ID: it.ID, Name: it.Name, Detail: it.Detail})
		}
	}
	u.opts.Settings.SetExcluded(list)
	u.applyExclusions()
}

// lockControl — the small lock at the end of an item's row.
//
//   - open and grey: not on the list; a click puts it there;
//   - closed and orange: on the list; a click takes it off;
//   - closed, no click: inside a folder on the list (that's changed in Settings);
//   - nothing: the provider protects it for its own reason anyway.
//
// Every case takes the same width, so the sizes before it stay in one column.
func (u *App) lockControl(it core.Item) fyne.CanvasObject {
	const size = 16
	icon := func(res fyne.Resource, c fyne.ThemeColorName) fyne.CanvasObject {
		return pad(4, 4, 4, 4, symbol(res, c, size))
	}

	switch {
	case it.Protected == nil:
		return container.NewCenter(newTappable(icon(lockOpen, colorSecondary), func() { u.addExclusions(it) }))
	case it.Protected.Key == reasonExcluded:
		return container.NewCenter(newTappable(icon(lockClosed, theme.ColorNameWarning), func() { u.allowCleaning(it) }))
	case it.Protected.Key == reasonExcludedFolder:
		return container.NewCenter(icon(lockClosed, colorSecondary))
	default:
		return gap(size+8, size+8)
	}
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
