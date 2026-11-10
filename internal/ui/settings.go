package ui

import (
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/settings"
)

// languageNames is how to name the languages in the list: each one in its own language.
var languageNames = map[string]string{
	"en": "English",
	"ru": "Русский",
}

// Links shown at the bottom of Settings.
const (
	websiteURL = "https://mrwd.github.io/products/disk-cleaner/"
	sourceURL  = "https://github.com/mrWD/disk-cleaner"
)

// donations are the same three places the website links to.
var donations = []struct{ name, url string }{
	{"Buy Me a Coffee", "https://buymeacoffee.com/ipupok"},
	{"Ko-fi", "https://ko-fi.com/ipupok"},
	{"PayPal", "https://www.paypal.com/donate/?hosted_button_id=VBNDB5AHYLGCY"},
}

// webLink is a blue link that opens raw in the default browser.
// The addresses are constants above, so parsing can't fail.
func webLink(label, raw string) *widget.Hyperlink {
	u, _ := url.Parse(raw)
	return widget.NewHyperlink(label, u)
}

// showSettings is the settings window: language, appearance, where to look for projects,
// and the dry run mode.
func (u *App) showSettings() {
	s := u.opts.Settings

	// --- Language: "Same as macOS" + all languages that have texts ---
	codes := append([]string{""}, u.tr.Languages()...)
	labels := make([]string, len(codes))
	for i, code := range codes {
		switch {
		case code == "":
			labels[i] = u.t("settings.language.system")
		case languageNames[code] != "":
			labels[i] = languageNames[code]
		default:
			labels[i] = code
		}
	}
	language := widget.NewSelect(labels, nil)
	language.SetSelectedIndex(max(slices.Index(codes, s.Language()), 0))

	// --- Appearance: three tiles, like System Settings in macOS ---
	appearance := u.newAppearancePicker(s.Appearance())

	// --- Project folders: a list with Remove, Add Folder… and Find (folders.go) ---
	roots := slices.Clone(s.Roots())
	rootsBox := u.projectFolderList(&roots, false)
	rootsHint := u.wrapLabel(u.t("settings.roots.hint"))
	rootsHint.Importance = widget.LowImportance

	// --- Dry run mode ---
	dry := widget.NewCheck(u.t("settings.dry_run"), nil)
	dry.SetChecked(s.DryRun())

	// --- Never clean: the list, edited here and saved with everything else ---
	never := slices.Clone(s.Excluded())
	neverBox := u.neverList(&never)

	// --- Permissions: switches with explanations (permissions.go) ---
	permissions, savePermissions := u.permissionsSection()

	// --- Updates: check at launch, Check Now (updates.go) ---
	var updates fyne.CanvasObject
	var checkUpdates *widget.Check
	if u.opts.Updater != nil {
		updates, checkUpdates = u.updatesSection()
	}

	// --- About: version (with the commit it was built from), project page, source ---
	about := container.NewHBox()
	version := u.app.Metadata().Version // empty in tests and with go run
	if u.opts.Updater != nil && u.opts.Updater.Version() != "" {
		version = strings.TrimSpace(version + " (" + u.opts.Updater.Version() + ")")
	}
	if version != "" {
		about.Add(widget.NewLabel(u.t("settings.version", map[string]any{"Version": version})))
	}
	about.Add(webLink("mrwd.github.io", websiteURL))
	about.Add(webLink("GitHub", sourceURL))

	// --- Support: donation links ---
	donate := container.NewHBox()
	for _, d := range donations {
		donate.Add(webLink(d.name, d.url))
	}
	supportHint := u.wrapLabel(u.t("settings.support.hint"))
	supportHint.Importance = widget.LowImportance

	form := widget.NewForm(
		widget.NewFormItem(u.t("settings.language"), language),
		widget.NewFormItem(u.t("settings.appearance"), appearance.box),
		widget.NewFormItem(u.t("settings.roots"), container.NewVBox(rootsHint, rootsBox)),
		widget.NewFormItem("", dry),
		widget.NewFormItem(u.t("settings.never"), neverBox),
		widget.NewFormItem(u.t("settings.permissions"), permissions),
	)
	if updates != nil {
		form.Append(u.t("settings.updates"), updates)
	}
	form.AppendItem(widget.NewFormItem(u.t("settings.about"), about))
	form.AppendItem(widget.NewFormItem(u.t("settings.support"), container.NewVBox(supportHint, donate)))

	// Save applies everything at once; Cancel throws the edits away.
	apply := func() {
		code := codes[max(language.SelectedIndex(), 0)]
		s.SetLanguage(code)
		u.tr.SetLanguage(code)
		s.SetDryRun(dry.Checked)

		if checkUpdates != nil {
			s.SetCheckUpdates(checkUpdates.Checked)
		}
		u.setAppearance(appearance.selected) // remember and apply
		downloadsChanged := savePermissions()

		if !slices.Equal(never, s.Excluded()) {
			s.SetExcluded(never)
			u.applyExclusions()
		}

		rootsChanged := !slices.Equal(roots, s.Roots())
		if rootsChanged || !s.RootsConfirmed() {
			s.SetRoots(roots) // saving also counts as confirming them
		}

		u.render() // rebuild the window in the new language
		if rootsChanged || downloadsChanged {
			u.scanAll()
		}
	}

	// A sheet rather than a Fyne dialog: the form is taller than the window
	// can be, and in a sheet it scrolls edge to edge (see sheet.go).
	var closeSheet func()
	cancel := widget.NewButtonWithIcon(u.t("confirm.cancel"), theme.CancelIcon(), func() { closeSheet() })
	save := widget.NewButtonWithIcon(u.t("settings.save"), theme.ConfirmIcon(), func() {
		closeSheet()
		apply()
	})
	save.Importance = widget.HighImportance
	closeSheet = u.showSheet(u.t("settings.title"), sheetScroll(form), 720, 700, cancel, save)
}

// neverList — the "Never clean" part of Settings: each entry with a Remove
// button, and Add Folder… below. It edits *list; Save writes it.
func (u *App) neverList(list *[]settings.Exclusion) fyne.CanvasObject {
	rows := container.NewVBox()

	var fill func()
	fill = func() {
		rows.Objects = nil
		if len(*list) == 0 {
			empty := u.wrapLabel(u.t("settings.never.empty"))
			empty.Importance = widget.LowImportance
			rows.Add(empty)
		}
		for i, e := range *list {
			rows.Add(u.listRow(e.Name, e.Detail, func() {
				*list = slices.Delete(*list, i, i+1)
				fill() // rebuild: the indexes of the rest have changed
			}))
		}
		rows.Refresh()
		u.refreshOverlays()
	}
	fill()

	// Add Folder…: everything inside the chosen folder will be skipped.
	add := widget.NewButtonWithIcon(u.t("settings.never.add"), theme.FolderOpenIcon(), func() {
		u.pickFolder(func(path string) {
			if slices.ContainsFunc(*list, func(e settings.Exclusion) bool { return e.ID == path }) {
				return
			}
			*list = append(*list, settings.Exclusion{ID: path, Name: filepath.Base(path), Detail: tildePath(path)})
			fill()
		})
	})

	return container.NewVBox(rows, container.NewHBox(add))
}
