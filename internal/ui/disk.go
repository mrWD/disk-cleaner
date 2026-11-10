package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
)

// fillDisk builds the disk card in the sidebar: the disk name, how much
// is free, a bar like macOS "Storage", and under it the "Clean All Safe" button.
//
// There are no per-section labels under the bar: how much each section found
// is already written to the right of its name in the list below, and the colour of a segment
// on the bar matches the colour of the section's icon.
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

// diskBar is the disk fill bar. The order of segments is as in macOS:
// first "other" (grey), then what was found per section (in their colours),
// and free space at the end.
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

// cleanEverywhere — the "Clean All Safe (4.4 GB)" button for every section.
// The label itself says what it covers and how much it frees, so no caption
// is needed. Only Safe items are included, see safeItems. While any section
// is still scanning the button is off: the plan would miss what hasn't been
// found yet, and the label would promise less than there is.
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

	label := u.t("button.clean_safe", map[string]any{"Size": size.String()})
	if scanning {
		label = u.t("button.clean_safe.scanning")
	}

	u.cleanEvery = widget.NewButtonWithIcon(label, theme.DeleteIcon(), func() { u.cleanAll(u.states...) })
	if scanning || count == 0 {
		u.cleanEvery.Disable()
	}
	return u.cleanEvery
}

// fillBanner is the strip above the right-hand part when dry run is on:
// it shows that the clean buttons will not delete anything right now.
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
