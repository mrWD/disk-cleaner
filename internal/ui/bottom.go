package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// updateBottom перестраивает нижнюю полосу: сколько выбрано, сколько
// освободится, кнопки «Снять все» и «Проверить и очистить».
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

	// Главная кнопка — синяя, как основное действие в диалогах macOS.
	u.cleanButton = widget.NewButtonWithIcon(u.t("button.review"), theme.DeleteIcon(), u.reviewAndClean)
	u.cleanButton.Importance = widget.HighImportance

	if count == 0 {
		deselect.Disable()
		u.cleanButton.Disable()
	}

	bar := pad(10, 10, 20, 20, container.NewBorder(nil, nil, summary, container.NewHBox(deselect, u.cleanButton)))

	bg := canvas.NewRectangle(theme.Color(colorSidebar))

	u.bottom.Objects = []fyne.CanvasObject{
		container.NewStack(bg, edges(hairline(), nil, nil, nil, bar)),
	}
	u.bottom.Refresh()
	u.relayout()
}
