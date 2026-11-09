package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
)

// fillHeader строит шапку: имя диска, сколько свободно, полоса как в
// «Хранилище» macOS (занято / найдено к очистке по разделам / свободно)
// и кнопки «Пересканировать» и «Настройки».
func (u *App) fillHeader() {
	if u.header == nil {
		return
	}

	rescan := widget.NewButtonWithIcon(u.t("button.rescan"), theme.ViewRefreshIcon(), u.scanAll)
	rescan.Importance = widget.LowImportance // плоская кнопка, как в панели инструментов
	openSettings := widget.NewButtonWithIcon(u.t("button.settings"), theme.SettingsIcon(), u.showSettings)
	openSettings.Importance = widget.LowImportance
	toolbar := container.NewHBox(rescan, openSettings)

	free, size, err := diskutil.FreeSpace("/")
	title := text(diskutil.VolumeName("Macintosh HD"), theme.SizeNameSubHeadingText, theme.ColorNameForeground, true)

	subtitle := u.t("free.unknown")
	if err == nil {
		subtitle = u.t("header.available", map[string]any{"Free": free.String(), "Total": size.String()})
	}

	info := container.NewVBox(title, text(subtitle, sizeCaption, colorSecondary, false))
	top := container.NewBorder(nil, nil, nil, container.NewCenter(toolbar), info)

	box := container.NewVBox(top)

	if err == nil {
		box.Add(u.diskBar(free, size))
	}

	if u.opts.Settings.DryRun() {
		box.Add(container.NewBorder(nil, nil,
			symbol(theme.WarningIcon(), theme.ColorNameWarning, 16), nil,
			colored(u.t("dry_run.banner"), theme.ColorNameWarning)))
	}

	u.header.Objects = []fyne.CanvasObject{
		pad(16, 6, 20, 20, card(box)),
	}
	u.header.Refresh()
}

// diskBar — полоса заполнения диска и подписи к ней.
func (u *App) diskBar(free, size core.Bytes) fyne.CanvasObject {
	used := float64(size - free)

	// Сколько нашёл каждый раздел — это часть занятого места.
	var parts []segment
	var legend []fyne.CanvasObject
	var found float64

	for _, st := range u.states {
		sum := total(st.items)
		if st.scanning || sum == 0 {
			continue
		}
		_, tint := u.look(st.provider)
		parts = append(parts, segment{size: float64(sum), color: tint})
		legend = append(legend, legendDot(tint, u.t(st.provider.ID()+".name"), sum.String()))
		found += float64(sum)
	}

	other := max(used-found, 0)
	usedColor := theme.Color(colorUsed)
	trackColor := theme.Color(colorTrack)

	// Порядок как в macOS: сначала «прочее», потом найденное, потом свободное.
	bar := storageBar(append(append([]segment{{size: other, color: usedColor}}, parts...),
		segment{size: float64(free), color: trackColor}))

	legend = append(legend,
		legendDot(usedColor, u.t("legend.other"), core.Bytes(other).String()),
		legendDot(trackColor, u.t("legend.available"), free.String()),
	)

	return container.NewVBox(gap(0, 2), bar, container.NewGridWithColumns(3, legend...))
}
