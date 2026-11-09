package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// languageNames — как называть языки в списке: каждый — на самом себе.
var languageNames = map[string]string{
	"en": "English",
	"ru": "Русский",
}

// showSettings — окно настроек: язык, оформление, где искать проекты,
// режим «только показать».
func (u *App) showSettings() {
	s := u.opts.Settings

	// --- Язык: «как в macOS» + все языки, для которых есть тексты ---
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

	// --- Оформление: три плитки, как в Системных настройках macOS ---
	appearance := u.newAppearancePicker(s.Appearance())

	// --- Папки для поиска worktree, по одной на строке ---
	roots := widget.NewMultiLineEntry()
	roots.SetText(strings.Join(s.Roots(), "\n"))
	roots.SetMinRowsVisible(3)
	rootsHint := u.wrapLabel(u.t("settings.roots.hint"))
	rootsHint.Importance = widget.LowImportance

	// --- Режим «только показать» ---
	dry := widget.NewCheck(u.t("settings.dry_run"), nil)
	dry.SetChecked(s.DryRun())

	form := widget.NewForm(
		widget.NewFormItem(u.t("settings.language"), language),
		widget.NewFormItem(u.t("settings.appearance"), appearance.box),
		widget.NewFormItem(u.t("settings.roots"), container.NewVBox(roots, rootsHint)),
		widget.NewFormItem("", dry),
	)

	d := dialog.NewCustomConfirm(u.t("settings.title"), u.t("settings.save"), u.t("confirm.cancel"), form,
		func(save bool) {
			if !save {
				return
			}

			code := codes[max(language.SelectedIndex(), 0)]
			s.SetLanguage(code)
			u.tr.SetLanguage(code)
			s.SetDryRun(dry.Checked)

			u.setAppearance(appearance.selected) // запомнить и применить

			newRoots := parseRoots(roots.Text)
			rootsChanged := !slices.Equal(newRoots, s.Roots())
			if rootsChanged {
				s.SetRoots(newRoots)
			}

			u.render() // перестроить окно на новом языке
			if rootsChanged {
				u.scanAll()
			}
		}, u.win)
	d.Resize(fyne.NewSize(660, 520))
	d.Show()
}

// parseRoots — строки из поля ввода → список папок. ~ раскрывается в домашнюю папку.
func parseRoots(text string) []string {
	home, _ := os.UserHomeDir()

	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "~" || strings.HasPrefix(line, "~/") {
			line = filepath.Join(home, strings.TrimPrefix(line, "~"))
		}
		out = append(out, filepath.Clean(line))
	}
	return out
}
