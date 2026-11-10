package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// updateBottom rebuilds the bottom strip: how many items are selected, how much
// will be freed, and the "Never Clean", "Deselect All" and "Review & Clean…" buttons.
func (u *App) updateBottom() {
	if u.bottom == nil {
		return
	}

	count := u.sel.Len()

	var summary fyne.CanvasObject
	if count == 0 {
		summary = container.NewCenter(text(u.t("bottom.none"), theme.SizeNameText, colorSecondary, false))
	} else {
		summary = container.NewVBox(
			text(u.t("bottom.selected", map[string]any{"Count": count}), theme.SizeNameText, theme.ColorNameForeground, true),
			text(u.t("bottom.size", map[string]any{"Size": u.sel.Total().String()}), sizeCaption, colorSecondary, false),
		)
	}

	deselect := widget.NewButton(u.t("button.deselect"), func() {
		u.sel.Clear()
		u.refreshDetails()
		u.updateBottom()
	})
	deselect.Importance = widget.LowImportance

	// Never Clean puts the selected items on the "Never clean" list (never.go).
	never := widget.NewButtonWithIcon(u.t("button.never_clean"), theme.VisibilityOffIcon(), u.neverClean)
	never.Importance = widget.LowImportance

	// The main button is blue, like the primary action in macOS dialogs.
	u.cleanButton = widget.NewButtonWithIcon(u.t("button.review"), theme.DeleteIcon(), u.reviewAndClean)
	u.cleanButton.Importance = widget.HighImportance

	if count == 0 {
		never.Disable()
		deselect.Disable()
		u.cleanButton.Disable()
	}

	bar := pad(10, 10, 20, 20, container.NewBorder(nil, nil, summary, container.NewHBox(never, deselect, u.cleanButton)))

	bg := canvas.NewRectangle(theme.Color(colorSidebar))

	u.bottom.Objects = []fyne.CanvasObject{
		container.NewStack(bg, edges(hairline(), nil, nil, nil, bar)),
	}
	u.bottom.Refresh()
	u.relayout()
}
