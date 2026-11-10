package ui

import (
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/access"
	"disk-cleaner/internal/core"
)

// Permissions: what Disk Cleaner needs, why, and the switches for it.
//
//   - Downloads folder. macOS protects it and asks the user the first time
//     the app reads it. The switch decides whether Disk Cleaner looks there
//     at all; when it's off, the folder isn't even opened.
//   - Documents and Desktop, only if a project folder is inside one of them:
//     macOS protects those the same way.
//   - The administrator password. macOS asks for it each time, and only to
//     remove simulator caches. The switch decides whether such items are
//     cleaned or skipped.
//
// macOS doesn't let an app grant or take back its own permissions: only the
// user can, in System Settings. So the switches control what Disk Cleaner
// uses, and each folder permission has a button to that page of System
// Settings. On first launch the same switches are shown before the first
// scan (showIntro), so macOS asks for access right after the user has read why.

// foldersNeedingAccess — Desktop and Documents, if a project folder is inside them.
func (u *App) foldersNeedingAccess() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var out []string
	for _, name := range []string{"Desktop", "Documents"} {
		dir := filepath.Join(home, name)
		for _, root := range u.opts.Settings.Roots() {
			if root == dir || inside(root, dir) {
				out = append(out, dir)
				break
			}
		}
	}
	return out
}

// permissionRow — one permission: the switch or title on the left, what
// macOS says about it on the right (status may be nil), why it's needed
// below, and optionally a button to System Settings.
func (u *App) permissionRow(control fyne.CanvasObject, why string, status fyne.CanvasObject, openSettings bool) fyne.CanvasObject {
	hint := u.wrapLabel(why)
	hint.Importance = widget.LowImportance

	parts := []fyne.CanvasObject{container.NewBorder(nil, nil, nil, status, control), hint}
	if openSettings {
		parts = append(parts, container.NewHBox(link(u.t("perm.open"), func() { _ = access.OpenSettings() })))
	}
	return container.NewVBox(parts...)
}

// setStatus changes a status label: text and color.
func setStatus(l *widget.Label, text string, importance widget.Importance) {
	l.Importance = importance
	l.SetText(text)
}

// checkFolder shows in l whether macOS lets Disk Cleaner read dir. The check
// may wait for the user to answer macOS's dialog, so it runs in the background.
func (u *App) checkFolder(dir string, l *widget.Label) {
	setStatus(l, u.t("perm.status.checking"), widget.LowImportance)
	go func() {
		granted := access.Folder(dir) == access.Granted
		fyne.Do(func() {
			if granted {
				setStatus(l, u.t("perm.status.granted"), widget.SuccessImportance)
			} else {
				setStatus(l, u.t("perm.status.denied"), widget.WarningImportance)
			}
		})
	}()
}

// permissionsSection — the Permissions part of Settings. It returns the
// widgets and a save function for the Save button, which reports whether
// the Downloads switch changed (then the sections are scanned again).
func (u *App) permissionsSection() (fyne.CanvasObject, func() bool) {
	s := u.opts.Settings
	home, _ := os.UserHomeDir()
	downloadsDir := filepath.Join(home, "Downloads")

	// Downloads: a switch and what macOS says. While the switch is off the
	// folder isn't checked: reading it would make macOS ask for access.
	downloadsStatus := widget.NewLabel("")
	downloads := widget.NewCheck(u.t("perm.downloads"), nil)
	downloads.SetChecked(s.UseDownloads())
	showDownloads := func() {
		if downloads.Checked {
			u.checkFolder(downloadsDir, downloadsStatus)
		} else {
			setStatus(downloadsStatus, u.t("perm.status.off"), widget.LowImportance)
		}
	}
	downloads.OnChanged = func(bool) { showDownloads() } // set after SetChecked, so it doesn't fire now
	showDownloads()

	rows := container.NewVBox(u.permissionRow(downloads, u.t("perm.downloads.why"), downloadsStatus, true))

	// Desktop and Documents, only when a project folder is inside them.
	for _, dir := range u.foldersNeedingAccess() {
		status := widget.NewLabel("")
		u.checkFolder(dir, status)
		args := map[string]any{"Folder": tildePath(dir)}
		rows.Add(u.permissionRow(widget.NewLabel(u.t("perm.folder", args)), u.t("perm.folder.why", args), status, true))
	}

	// The administrator password: macOS asks for it every time, so there's
	// no lasting state to show, only the switch.
	admin := widget.NewCheck(u.t("perm.admin"), nil)
	admin.SetChecked(s.AllowAdmin())
	adminStatus := widget.NewLabel(u.t("perm.status.each_time"))
	adminStatus.Importance = widget.LowImportance
	rows.Add(u.permissionRow(admin, u.t("perm.admin.why"), adminStatus, false))

	note := u.wrapLabel(u.t("perm.note"))
	note.Importance = widget.LowImportance
	rows.Add(note)

	save := func() bool {
		changed := downloads.Checked != s.UseDownloads()
		s.SetUseDownloads(downloads.Checked)
		s.SetAllowAdmin(admin.Checked)
		return changed
	}
	return rows, save
}

// showIntro — the window on first launch: which permissions Disk Cleaner
// uses and why, with the same switches as in Settings. The first scan starts
// only after Continue.
func (u *App) showIntro() {
	s := u.opts.Settings

	downloads := widget.NewCheck(u.t("perm.downloads"), nil)
	downloads.SetChecked(s.UseDownloads())
	admin := widget.NewCheck(u.t("perm.admin"), nil)
	admin.SetChecked(s.AllowAdmin())

	content := container.NewVBox(
		u.wrapLabel(u.t("intro.text")),
		u.permissionRow(downloads, u.t("perm.downloads.why")+" "+u.t("intro.downloads_note"), nil, false),
		u.permissionRow(admin, u.t("perm.admin.why"), nil, false),
	)

	// Our own blue Continue button instead of the dialog's grey one: it's the
	// only way forward, so it looks like the main action.
	var d dialog.Dialog
	next := widget.NewButton(u.t("intro.continue"), func() {
		d.Hide()
		u.finishIntro(downloads.Checked, admin.Checked)
	})
	next.Importance = widget.HighImportance
	content.Add(gap(0, 6))
	content.Add(container.NewCenter(next))

	d = dialog.NewCustomWithoutButtons(u.t("intro.title"), content, u.win)
	d.Resize(fyne.NewSize(620, 380))
	d.Show()
}

// finishIntro saves the choices from the first-launch window and starts the
// first scan. If Downloads is on, that scan reads it and macOS asks for access.
func (u *App) finishIntro(useDownloads, allowAdmin bool) {
	s := u.opts.Settings
	s.SetUseDownloads(useDownloads)
	s.SetAllowAdmin(allowAdmin)
	s.SetPermissionsAsked(true)
	u.scanAll()
}

// withoutAdmin — sel without the items whose cleaning needs the administrator
// password, and how many were left out. Used when that's turned off.
func (u *App) withoutAdmin(sel *core.Selection) (*core.Selection, int) {
	out := core.NewSelection()
	skipped := 0
	for _, p := range u.providers() {
		for _, it := range sel.ItemsFor(p) {
			if needsAdmin(p, it) {
				skipped++
				continue
			}
			_ = out.Add(it) // it was selectable before, so it still is
		}
	}
	return out, skipped
}

// needsAdmin plans one item on its own to see whether any of its actions
// needs the password. Plan only describes actions, it doesn't run anything.
func needsAdmin(p core.Provider, it core.Item) bool {
	actions, err := p.Plan([]core.Item{it})
	if err != nil {
		return false // BuildPlan will report the same error properly
	}
	for _, a := range actions {
		if a.NeedsAdmin {
			return true
		}
	}
	return false
}
