package ui

import (
	"errors"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
)

// sidebarWidth is the width of the sidebar, like in Finder.
const sidebarWidth = 270

// buildSidebar builds the sidebar from top to bottom:
//   - the app icon and name;
//   - a search field over all sections (like in System Settings);
//   - section rows with coloured icons; they scroll if
//     the window is short;
//   - at the bottom, the disk card: how much is free and the "Clean All Safe" button (disk.go),
//     and under it the "Rescan" and "Settings" buttons.
func (u *App) buildSidebar() fyne.CanvasObject {
	u.sidebarRows = container.NewVBox()
	u.disk = container.NewVBox()
	u.searchEntry = searchField(u.t("search.global"), u.globalQuery, u.setGlobalQuery)

	logo := canvas.NewImageFromResource(fyne.NewStaticResource("appicon.png", appIcon))
	logo.FillMode = canvas.ImageFillContain
	appName := text(u.t("app.title"), theme.SizeNameSubHeadingText, theme.ColorNameForeground, true)
	head := container.NewHBox(container.NewGridWrap(fyne.NewSize(30, 30), logo), container.NewCenter(appName))

	section := text(u.t("sidebar.section"), theme.SizeNameCaptionText, colorSecondary, true)

	top := container.NewVBox(
		head,
		gap(0, 4),
		u.searchEntry,
		gap(0, 4),
		// Space below: the section rows live in a separate scroll area, and
		// the VBox no longer adds its own gap between the caption and them.
		pad(0, 6, 8, 0, section),
	)

	// At the bottom of the panel: the disk card and under it flat buttons, like in a toolbar.
	rescan := widget.NewButtonWithIcon(u.t("button.rescan"), theme.ViewRefreshIcon(), u.scanAll)
	rescan.Importance = widget.LowImportance
	openSettings := widget.NewButtonWithIcon(u.t("button.settings"), theme.SettingsIcon(), u.showSettings)
	openSettings.Importance = widget.LowImportance
	footer := container.NewVBox(
		pad(8, 0, 0, 0, u.disk),
		pad(4, 0, 0, 0, container.NewHBox(rescan, layout.NewSpacer(), openSettings)),
	)

	// edges: the header on top, the disk and buttons at the bottom, and the section rows take everything
	// that is left and scroll if they don't fit.
	list := edges(top, footer, nil, nil, container.NewVScroll(u.sidebarRows))

	bg := canvas.NewRectangle(theme.Color(colorSidebar))
	divider := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	divider.SetMinSize(fyne.NewSize(1, 1))

	return container.NewStack(
		bg,
		edges(nil, nil, nil, divider,
			pad(16, 10, 10, 10, container.NewStack(gap(sidebarWidth, 0), list))),
	)
}

// fillSidebar rebuilds the section rows (there are only a few of them, so it is cheap).
func (u *App) fillSidebar() {
	if u.sidebarRows == nil {
		return
	}

	u.sidebarRows.Objects = nil
	for i, st := range u.states {
		u.sidebarRows.Add(u.sidebarRow(i, st))
	}
	u.sidebarRows.Refresh()
}

// sidebarRow is one row: an icon, a name, and on the right the size or the state.
// During a search the right side shows how much was found in the section, and sections with
// no matches are dimmed.
func (u *App) sidebarRow(i int, st *providerState) fyne.CanvasObject {
	icon, tint := u.look(st.provider)
	selected := i == u.current && !u.searching()

	nameColor := theme.ColorNameForeground
	statusText := u.statusText(st)
	statusColor := colorSecondary
	if st.err != nil && !isUnavailable(st.err) {
		statusColor = theme.ColorNameError
	}

	if u.searching() && !st.scanning {
		var found core.Bytes
		for _, g := range u.groupsOf(st, u.globalQuery) {
			found += total(g.items)
		}
		statusText = found.String()
		if found == 0 {
			statusText = "–"
			nameColor = colorSecondary
		}
	}

	name := text(u.t(st.provider.ID()+".name"), theme.SizeNameText, nameColor, selected)
	var status fyne.CanvasObject = text(statusText, sizeCaption, statusColor, false)
	if st.scanning {
		status = u.spinner(st) // instead of the word "Scanning…"
	}

	row := pad(5, 5, 6, 8, container.NewBorder(nil, nil,
		container.NewHBox(badge(icon, tint, 22), container.NewCenter(name)),
		container.NewCenter(status),
	))

	// The selected row sits on a rounded backing.
	var content fyne.CanvasObject = row
	if selected {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNameSelection))
		bg.CornerRadius = theme.Size(theme.SizeNameSelectionRadius)
		content = container.NewStack(bg, row)
	}

	return newTappable(content, func() { u.openSection(i) })
}

// openSection opens a section: it leaves the global search (if it
// was on) and selects the section on the left.
func (u *App) openSection(i int) {
	if u.searching() {
		u.globalQuery = ""
		u.searchEntry.SetText("") // the field gets cleared, and the repeated setGlobalQuery call will do nothing
	}
	u.selectProvider(i)
}

// selectProvider selects a section on the left.
func (u *App) selectProvider(i int) {
	u.current = i
	u.detailsScroll = nil // a new section starts from the top
	u.fillSidebar()
	u.refreshDetails()
}

// statusText is what to write to the right of the section name.
func (u *App) statusText(st *providerState) string {
	switch {
	case st.scanning:
		return u.t("status.scanning")
	case isUnavailable(st.err):
		return u.t("status.unavailable")
	case st.err != nil:
		return u.t("status.error")
	case len(st.items) == 0:
		return u.t("status.empty")
	case total(st.items) == 0:
		return u.t("status.protected_only") // something was found, but all of it is protected
	}
	return total(st.items).String()
}

// isUnavailable: the provider did not fail, it is just unavailable right now.
func isUnavailable(err error) bool {
	var u *core.UnavailableError
	return errors.As(err, &u)
}
