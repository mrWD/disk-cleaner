package ui

import (
	"errors"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"disk-cleaner/internal/core"
)

// sidebarWidth — ширина боковой панели, как в Finder.
const sidebarWidth = 270

// buildSidebar — боковая панель: значок и имя приложения, поле поиска
// по всем разделам (как в Системных настройках), подзаголовок и строки
// разделов с цветными значками.
func (u *App) buildSidebar() fyne.CanvasObject {
	u.sidebarRows = container.NewVBox()
	u.searchEntry = searchField(u.t("search.global"), u.globalQuery, u.setGlobalQuery)

	logo := canvas.NewImageFromResource(fyne.NewStaticResource("appicon.png", appIcon))
	logo.FillMode = canvas.ImageFillContain
	appName := text(u.t("app.title"), theme.SizeNameSubHeadingText, theme.ColorNameForeground, true)
	head := container.NewHBox(container.NewGridWrap(fyne.NewSize(30, 30), logo), container.NewCenter(appName))

	section := text(u.t("sidebar.section"), theme.SizeNameCaptionText, colorSecondary, true)

	list := container.NewVBox(
		head,
		gap(0, 4),
		u.searchEntry,
		gap(0, 4),
		pad(0, 0, 8, 0, section),
		u.sidebarRows,
	)

	bg := canvas.NewRectangle(theme.Color(colorSidebar))
	divider := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	divider.SetMinSize(fyne.NewSize(1, 1))

	return container.NewStack(
		bg,
		edges(nil, nil, nil, divider,
			pad(16, 16, 10, 10, container.NewStack(gap(sidebarWidth, 0), list))),
	)
}

// fillSidebar перестраивает строки разделов (их всего несколько — дёшево).
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

// sidebarRow — одна строка: значок, имя, справа размер или состояние.
// Во время поиска справа — сколько найдено в разделе, а разделы без
// совпадений приглушены.
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
	status := text(statusText, sizeCaption, statusColor, false)

	row := pad(5, 5, 6, 8, container.NewBorder(nil, nil,
		container.NewHBox(badge(icon, tint, 22), container.NewCenter(name)),
		container.NewCenter(status),
	))

	// Выбранная строка — на скруглённой подложке.
	var content fyne.CanvasObject = row
	if selected {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNameSelection))
		bg.CornerRadius = theme.Size(theme.SizeNameSelectionRadius)
		content = container.NewStack(bg, row)
	}

	return newTappable(content, func() { u.openSection(i) })
}

// openSection открывает раздел: выходит из глобального поиска (если он
// был) и выбирает раздел слева.
func (u *App) openSection(i int) {
	if u.searching() {
		u.globalQuery = ""
		u.searchEntry.SetText("") // поле очистится, повторный вызов setGlobalQuery ничего не сделает
	}
	u.selectProvider(i)
}

// selectProvider выбирает раздел слева.
func (u *App) selectProvider(i int) {
	u.current = i
	u.detailsScroll = nil // новый раздел — с начала
	u.fillSidebar()
	u.refreshDetails()
}

// statusText — что написать справа от имени раздела.
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
		return u.t("status.protected_only") // что-то нашлось, но всё защищено
	}
	return total(st.items).String()
}

// isUnavailable — провайдер не ошибся, а просто сейчас недоступен.
func isUnavailable(err error) bool {
	var u *core.UnavailableError
	return errors.As(err, &u)
}
