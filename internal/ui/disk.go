package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
)

// fillDisk строит плашку диска в боковой панели: имя диска, сколько
// свободно, полоса как в «Хранилище» macOS и под ней кнопка «Очистить всё».
//
// Подписей с разделами под полосой нет: сколько нашёл каждый раздел,
// и так написано справа от его имени в списке ниже, а цвет отрезка
// на полосе совпадает с цветом значка раздела.
func (u *App) fillDisk() {
	if u.disk == nil {
		return
	}

	free, size, err := diskutil.FreeSpace("/")

	subtitle := u.t("free.unknown")
	if err == nil {
		subtitle = u.t("disk.available", map[string]any{"Free": free.String(), "Total": size.String()})
	}

	box := container.NewVBox(
		text(diskutil.VolumeName("Macintosh HD"), theme.SizeNameText, theme.ColorNameForeground, true),
		text(subtitle, sizeCaption, colorSecondary, false),
	)
	if err == nil {
		box.Add(gap(0, 2))
		box.Add(u.diskBar(free, size))
	}
	box.Add(gap(0, 6))
	box.Add(u.cleanEverywhere())

	u.disk.Objects = []fyne.CanvasObject{card(box)}
	u.disk.Refresh()
}

// diskBar — полоса заполнения диска. Порядок отрезков как в macOS:
// сначала «прочее» (серое), потом найденное по разделам (их цветами),
// в конце свободное место.
func (u *App) diskBar(free, size core.Bytes) fyne.CanvasObject {
	used := float64(size - free)

	var parts []segment
	var found float64
	for _, st := range u.states {
		sum := total(st.items)
		if st.scanning || sum == 0 {
			continue
		}
		_, tint := u.look(st.provider)
		parts = append(parts, segment{size: float64(sum), color: tint})
		found += float64(sum)
	}

	other := max(used-found, 0)
	all := append([]segment{{size: other, color: theme.Color(colorUsed)}}, parts...)
	all = append(all, segment{size: float64(free), color: theme.Color(colorTrack)})
	return storageBar(all)
}

// cleanEverywhere — кнопка «Очистить всё» для всех разделов и подпись
// под ней: сколько это освободит. Входит только безопасное (Safe), см. safeItems.
// Пока хоть один раздел сканируется, кнопка недоступна: иначе в план
// не попало бы то, что ещё не нашлось, а подпись обещала бы меньше.
func (u *App) cleanEverywhere() fyne.CanvasObject {
	var size core.Bytes
	var count int
	scanning := false
	for _, st := range u.states {
		scanning = scanning || st.scanning
		for _, it := range safeItems(st) {
			size += it.Size
			count++
		}
	}

	u.cleanEvery = widget.NewButtonWithIcon(u.t("button.clean_all"), theme.DeleteIcon(), func() { u.cleanAll(u.states...) })

	caption := u.t("clean_all.caption", map[string]any{"Size": size.String()})
	switch {
	case scanning:
		u.cleanEvery.Disable()
		caption = u.t("clean_all.scanning")
	case count == 0:
		u.cleanEvery.Disable()
		caption = u.t("clean_all.nothing")
	}

	return container.NewVBox(u.cleanEvery, container.NewCenter(text(caption, sizeCaption, colorSecondary, false)))
}

// fillBanner — полоса над правой частью, когда включён пробный режим:
// так видно, что кнопки очистки сейчас ничего не удалят.
func (u *App) fillBanner() {
	if u.banner == nil {
		return
	}

	u.banner.Objects = nil
	if u.opts.Settings.DryRun() {
		u.banner.Add(pad(14, 0, 20, 20, container.NewBorder(nil, nil,
			symbol(theme.WarningIcon(), theme.ColorNameWarning, 16), nil,
			colored(u.t("dry_run.banner"), theme.ColorNameWarning))))
	}
	u.banner.Refresh()
}
