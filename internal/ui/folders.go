package ui

import (
	"os"
	"path/filepath"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Lists of folders the user edits: the project folders (here) and the
// "Never clean" list (settings.go). Both show one row per entry with a
// Remove button, and Add Folder… below.

// listRow — one entry: a main line, an optional grey second line, and Remove
// on the right. Long text is cut with "…" so it never widens the window.
func (u *App) listRow(main, detail string, onRemove func()) fyne.CanvasObject {
	name := widget.NewLabel(main)
	name.Truncation = fyne.TextTruncateEllipsis
	lines := container.New(layout.NewCustomPaddedVBoxLayout(0), name)
	if detail != "" {
		d := widget.NewLabel(detail)
		d.Truncation = fyne.TextTruncateEllipsis
		d.Importance = widget.LowImportance
		lines.Add(d)
	}

	remove := widget.NewButton(u.t("settings.never.remove"), onRemove)
	remove.Importance = widget.LowImportance
	return container.NewBorder(nil, nil, nil, container.NewCenter(remove), lines)
}

// pickFolder opens a folder chooser in the home folder and calls onPick with
// the chosen path. Nothing happens if the user cancels.
func (u *App) pickFolder(onPick func(path string)) {
	pick := dialog.NewFolderOpen(func(dir fyne.ListableURI, err error) {
		if err != nil || dir == nil {
			return // cancelled
		}
		onPick(filepath.Clean(dir.Path()))
	}, u.win)

	if home, err := os.UserHomeDir(); err == nil {
		if start, err := storage.ListerForURI(storage.NewFileURI(home)); err == nil {
			pick.SetLocation(start)
		}
	}
	pick.Resize(fyne.NewSize(760, 520))
	pick.Show()
}

// projectFolderList — the editable list of project folders. It edits *list
// in place; the caller saves it. With a FindProjectFolders function in the
// options, there's also a Find Project Folders button that adds what it
// finds (folders in ~ that have git repositories). If findNow is true, it
// searches right away and starts the list from what it finds.
func (u *App) projectFolderList(list *[]string, findNow bool) fyne.CanvasObject {
	rows := container.NewVBox()
	status := widget.NewLabel("")
	status.Importance = widget.LowImportance
	status.Wrapping = fyne.TextWrapWord
	status.Hide()

	var fill func()
	fill = func() {
		rows.Objects = nil
		if len(*list) == 0 {
			empty := u.wrapLabel(u.t("folders.none"))
			empty.Importance = widget.LowImportance
			rows.Add(empty)
		}
		for i, dir := range *list {
			rows.Add(u.listRow(tildePath(dir), "", func() {
				*list = slices.Delete(*list, i, i+1)
				fill() // rebuild: the indexes of the rest have changed
			}))
		}
		rows.Refresh()
		u.refreshOverlays()
	}

	add := func(dirs ...string) {
		for _, d := range dirs {
			if !slices.Contains(*list, d) {
				*list = append(*list, d)
			}
		}
		fill()
	}

	buttons := container.NewHBox(widget.NewButtonWithIcon(u.t("settings.never.add"), theme.FolderOpenIcon(), func() {
		u.pickFolder(func(path string) { add(path) })
	}))

	// find runs the search in the background: it walks a few levels of every
	// folder in ~, which can take a moment on a big disk.
	find := func(replace bool) {
		status.SetText(u.t("folders.finding"))
		status.Show()
		u.refreshOverlays()
		go func() {
			found := u.opts.FindProjectFolders()
			fyne.Do(func() {
				status.Hide()
				if replace {
					*list = nil
				}
				add(found...)
				if len(found) == 0 {
					status.SetText(u.t("folders.found_none"))
					status.Show()
					u.refreshOverlays()
				}
			})
		}()
	}

	if u.opts.FindProjectFolders != nil {
		findBtn := widget.NewButtonWithIcon(u.t("folders.find"), theme.SearchIcon(), func() { find(false) })
		findBtn.Importance = widget.LowImportance
		buttons.Add(findBtn)
	}

	fill()
	if findNow && u.opts.FindProjectFolders != nil {
		find(true)
	}
	return container.NewVBox(rows, status, buttons)
}
