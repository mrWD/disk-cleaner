package ui

import (
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
)

// Правая часть окна состоит из двух частей:
//   - шапка (detailsTop): крупный заголовок, поле поиска и «Выбрать все».
//     Она НЕ перестраивается при каждом нажатии клавиши — иначе поле
//     поиска пересоздавалось бы и теряло фокус;
//   - список (list): плашки категорий. Он перестраивается целиком.
//
// По умолчанию всё компактно: категория — одна строка заголовка,
// объект — одна строка (галочка, имя, путь, метка состояния, размер).
// Клик по заголовку категории раскрывает объяснения и подробности.

// checkIndent — насколько сдвинуть подпись под галочкой, чтобы она
// начиналась ровно под текстом, а не под квадратиком.
const checkIndent = 31

// viewKey — что сейчас показано справа: результаты поиска или раздел.
func (u *App) viewKey() string {
	if u.searching() {
		return "search"
	}
	return fmt.Sprintf("section:%d", u.current)
}

// refreshDetails обновляет правую часть: шапку — только если сменился
// вид, список — всегда, сохраняя позицию прокрутки.
func (u *App) refreshDetails() {
	if u.details == nil || len(u.states) == 0 {
		return
	}

	// Выбор принадлежит тому, что видно. Перешли в другой раздел или в
	// поиск — отметки снимаются: иначе в нижней полосе оставались бы
	// объекты, которых на экране нет, и их легко было бы удалить случайно.
	// Смена языка или оформления вид не меняет — там отметки остаются.
	if key := u.viewKey(); key != u.selView {
		u.selView = key
		if u.sel.Len() > 0 {
			u.sel.Clear()
			u.updateBottom()
		}
	}

	if key := u.viewKey(); key != u.topKey || u.detailsTop == nil {
		u.topKey = key
		u.detailsTop = u.buildTop()
		u.list = container.NewStack()
		u.detailsScroll = nil
		u.details.Objects = []fyne.CanvasObject{edges(u.detailsTop, nil, nil, nil, u.list)}
		u.details.Refresh()
	}
	u.updateTop()

	var offset fyne.Position
	if u.detailsScroll != nil {
		offset = u.detailsScroll.Offset
	}

	var body fyne.CanvasObject
	if u.searching() {
		body = u.buildSearchResults()
	} else {
		body = u.buildSection(u.states[u.current])
	}

	scroll := container.NewVScroll(pad(2, 24, 20, 20, body))
	scroll.Offset = offset
	u.detailsScroll = scroll

	u.list.Objects = []fyne.CanvasObject{scroll}
	u.list.Refresh()
}

// buildTop — шапка: значок и крупный заголовок, подзаголовок, ниже
// поле поиска по разделу и кнопки «Выбрать все» и «Очистить всё».
func (u *App) buildTop() *fyne.Container {
	var icon []byte
	tint := theme.Color(colorUsed)
	name := u.t("search.title")
	var search fyne.CanvasObject = gap(0, 0)
	u.cleanAllBtn = nil

	if !u.searching() {
		st := u.states[u.current]
		// «Очистить всё» — всё безопасное в этом разделе. Сначала всё равно
		// откроется окно проверки со списком команд.
		u.cleanAllBtn = widget.NewButtonWithIcon(u.t("button.clean_all"), theme.DeleteIcon(), func() { u.cleanAll(st) })
		icon, tint = u.look(st.provider)
		name = u.t(st.provider.ID() + ".name")
		u.sectionQuery = "" // новый раздел — поиск с чистого листа
		search = searchField(u.t("search.section", map[string]any{"Section": name}), "", u.setSectionQuery)
	}

	// Заголовок и подзаголовок — простые надписи canvas.Text: у них нет
	// внутренних отступов (в отличие от Label и RichText), поэтому обе строки
	// начинаются ровно от одного края и стоят плотно друг под другом.
	title := text(name, sizeTitle, theme.ColorNameForeground, true)
	u.subtitle = text("", theme.SizeNameText, colorSecondary, false)

	var mark fyne.CanvasObject
	if icon != nil {
		mark = badge(icon, tint, 48)
	} else {
		mark = container.NewGridWrap(fyne.NewSize(48, 48),
			container.NewStack(roundedRect(tint, 11), pad(11, 11, 11, 11, symbol(theme.SearchIcon(), theme.ColorNameForegroundOnPrimary, 26))))
	}

	// Значок слева, справа — две строки, по вертикали ровно по центру значка:
	// пустые «распорки» (Spacer) сверху и снизу делят свободное место поровну.
	lines := container.New(layout.NewCustomPaddedVBoxLayout(2), title, u.subtitle)
	heading := container.New(layout.NewCustomPaddedHBoxLayout(14),
		mark,
		container.NewVBox(layout.NewSpacer(), lines, layout.NewSpacer()),
	)

	u.selectAllBtn = widget.NewButtonWithIcon(u.t("button.select_all"), theme.CheckButtonCheckedIcon(), u.toggleVisibleSelection)
	buttons := container.NewHBox(u.selectAllBtn)
	if u.cleanAllBtn != nil {
		buttons.Add(u.cleanAllBtn)
	}
	toolbar := container.NewBorder(nil, nil, nil, buttons, search)

	return pad(14, 8, 20, 20, container.NewVBox(heading, gap(0, 4), toolbar))
}

// updateTop меняет в шапке то, что зависит от данных: подзаголовок,
// надпись «Выбрать все / Снять все» и доступность «Очистить всё».
// Само поле поиска не трогается.
func (u *App) updateTop() {
	if u.subtitle == nil {
		return
	}

	setText(u.subtitle, u.subtitleText())

	if u.cleanAllBtn != nil {
		if len(safeItems(u.states[u.current])) == 0 {
			u.cleanAllBtn.Disable() // безопасного нет (или раздел ещё сканируется)
		} else {
			u.cleanAllBtn.Enable()
		}
	}

	items := u.selectableVisible()
	switch {
	case len(items) == 0:
		u.selectAllBtn.SetText(u.t("button.select_all"))
		u.selectAllBtn.Disable()
	case u.allSelected(items):
		u.selectAllBtn.SetText(u.t("button.deselect"))
		u.selectAllBtn.SetIcon(theme.CheckButtonIcon())
		u.selectAllBtn.Enable()
	default:
		u.selectAllBtn.SetText(u.t("button.select_all"))
		u.selectAllBtn.SetIcon(theme.CheckButtonCheckedIcon())
		u.selectAllBtn.Enable()
	}
}

// subtitleText — строка под заголовком.
func (u *App) subtitleText() string {
	if u.searching() {
		var count int
		var size core.Bytes
		for _, g := range u.visibleGroups() {
			count += len(g.items)
			size += total(g.items)
		}
		return u.t(plural("search.subtitle", count), map[string]any{
			"Count": count, "Size": size.String(), "Query": strings.TrimSpace(u.globalQuery),
		})
	}

	st := u.states[u.current]
	var unavailable *core.UnavailableError
	switch {
	case st.scanning:
		return u.t("details.scanning")
	case errors.As(st.err, &unavailable):
		return u.reason(unavailable.Reason)
	case st.err != nil:
		return u.t("details.error", map[string]any{"Error": st.err.Error()})
	case len(st.items) == 0:
		return u.t("details.nothing")
	case total(st.items) == 0:
		return u.t("details.protected_only")
	}
	return u.t(plural("details.subtitle", len(st.items)), map[string]any{
		"Size": total(st.items).String(), "Count": len(st.items),
	})
}

// buildSection — список открытого раздела: плашки категорий.
func (u *App) buildSection(st *providerState) fyne.CanvasObject {
	if st.scanning {
		return widget.NewProgressBarInfinite()
	}

	groups := u.groupsOf(st, u.sectionQuery)
	var out []fyne.CanvasObject
	for _, g := range groups {
		out = append(out, u.buildCategory(g))
	}

	query := strings.TrimSpace(u.sectionQuery)
	switch {
	case query != "" && len(groups) == 0 && len(st.items) > 0:
		out = append(out, secondary(u.t("search.nothing", map[string]any{"Query": query}), true))

	case query == "" && len(groups) > 0:
		// Пустые категории — одной строкой внизу: видно, что их проверили.
		var empty []string
		for _, cat := range st.provider.Categories() {
			if len(itemsOf(st.items, cat.ID)) == 0 {
				empty = append(empty, u.t(cat.TitleKey()))
			}
		}
		if len(empty) > 0 {
			out = append(out, pad(0, 0, 4, 0, secondary(u.t("details.also_checked", map[string]any{"List": strings.Join(empty, ", ")}), true)))
		}
	}

	return container.NewVBox(out...)
}

// buildSearchResults — результаты глобального поиска по разделам:
// заголовок раздела со значком и под ним его найденные категории.
func (u *App) buildSearchResults() fyne.CanvasObject {
	var out []fyne.CanvasObject
	var scanning []string

	for i, st := range u.states {
		if st.scanning {
			scanning = append(scanning, u.t(st.provider.ID()+".name"))
			continue
		}
		groups := u.groupsOf(st, u.globalQuery)
		if len(groups) == 0 {
			continue
		}

		var size core.Bytes
		for _, g := range groups {
			size += total(g.items)
		}

		// Заголовок раздела: клик открывает сам раздел.
		icon, tint := u.look(st.provider)
		open := newTappable(pad(4, 4, 4, 4, container.NewBorder(nil, nil,
			container.NewHBox(badge(icon, tint, 22), container.NewCenter(text(u.t(st.provider.ID()+".name"), theme.SizeNameSubHeadingText, theme.ColorNameForeground, true))),
			container.NewCenter(text(size.String(), theme.SizeNameText, colorSecondary, false)),
		)), func() { u.openSection(i) })
		out = append(out, pad(6, 0, 0, 0, open))

		for _, g := range groups {
			out = append(out, u.buildCategory(g))
		}
	}

	if len(out) == 0 {
		out = append(out, secondary(u.t("search.nothing", map[string]any{"Query": strings.TrimSpace(u.globalQuery)}), true))
	}
	if len(scanning) > 0 {
		out = append(out, secondary(u.t("search.still_scanning", map[string]any{"List": strings.Join(scanning, ", ")}), true))
	}

	return container.NewVBox(out...)
}

// buildCategory — плашка категории. Свёрнутая (по умолчанию): одна
// строка заголовка и по строке на объект. Развёрнутая: плюс объяснения
// «что это / почему можно / что будет» и полные подробности объектов.
func (u *App) buildCategory(g group) fyne.CanvasObject {
	cat, items := g.cat, g.items
	open := u.expanded[cat.ID]

	// Галочка категории: выбраны все — галочка, часть — черта, ничего — пусто.
	var selectable []core.Item
	for _, it := range items {
		if ok, _ := it.CanSelect(); ok {
			selectable = append(selectable, it)
		}
	}
	groupCheck := widget.NewCheck("", nil)
	chosen := 0
	for _, it := range selectable {
		if u.sel.Contains(it) {
			chosen++
		}
	}
	groupCheck.Checked = chosen > 0 && chosen == len(selectable)
	groupCheck.Partial = chosen > 0 && chosen < len(selectable)
	groupCheck.OnChanged = func(on bool) {
		for _, it := range selectable {
			if on {
				_ = u.sel.Add(it)
			} else {
				u.sel.Remove(it)
			}
		}
		u.refreshDetails()
		u.updateBottom()
	}
	if len(selectable) == 0 {
		groupCheck.Disable()
	}

	// Остальной заголовок — кликабельный: раскрыть или свернуть.
	chevron := theme.MenuExpandIcon()
	if open {
		chevron = theme.MenuDropDownIcon()
	}
	title := text(u.t(cat.TitleKey()), theme.SizeNameSubHeadingText, theme.ColorNameForeground, true)
	count := text(u.t(plural("category.summary", len(items)), map[string]any{"Count": len(items)}), sizeCaption, colorSecondary, false)
	size := text(totalAll(items).String(), theme.SizeNameSubHeadingText, theme.ColorNameForeground, true)

	toggle := newTappable(pad(2, 2, 0, 4, container.NewBorder(nil, nil,
		container.NewHBox(container.NewCenter(title), u.riskPill(cat.Risk)),
		container.NewHBox(container.NewCenter(count), container.NewCenter(size), container.NewCenter(symbol(chevron, colorSecondary, 16))),
	)), func() {
		u.expanded[cat.ID] = !open
		u.refreshDetails()
	})

	parts := []fyne.CanvasObject{container.NewBorder(nil, nil, groupCheck, nil, toggle)}

	if open {
		// Требование: рядом с кнопками — что это и почему это можно удалить.
		parts = append(parts, pad(0, 4, checkIndent, 0, container.NewVBox(
			explainRow(theme.InfoIcon(), u.t("label.what"), u.t(cat.WhatKey())),
			explainRow(theme.ConfirmIcon(), u.t("label.why"), u.t(cat.WhyKey())),
			explainRow(theme.HistoryIcon(), u.t("label.after"), u.t(cat.AfterKey())),
		)))
	}

	parts = append(parts, hairline())
	for i, it := range items {
		if i > 0 {
			parts = append(parts, pad(0, 0, checkIndent, 0, hairline()))
		}
		parts = append(parts, u.itemRow(it, open))
	}

	return pad(0, 8, 0, 0, card(container.NewVBox(parts...)))
}

// explainRow — строка объяснения: маленький серый значок и текст
// «Заголовок. Пояснение», где заголовок чуть темнее.
func explainRow(icon fyne.Resource, heading, body string) fyne.CanvasObject {
	rt := widget.NewRichText(
		&widget.TextSegment{Text: heading + " ", Style: widget.RichTextStyle{
			ColorName: theme.ColorNameForeground, SizeName: sizeCaption, Inline: true,
			TextStyle: fyne.TextStyle{Bold: true},
		}},
		&widget.TextSegment{Text: body, Style: widget.RichTextStyle{
			ColorName: colorSecondary, SizeName: sizeCaption, Inline: true,
		}},
	)
	rt.Wrapping = fyne.TextWrapWord

	return container.NewBorder(nil, nil,
		pad(7, 0, 0, 0, container.NewVBox(symbol(icon, colorSecondary, 14))),
		nil, rt)
}

// itemRow — строка объекта.
//
// Компактно — одна строка: галочка с именем, серым путь (обрезается «…»),
// оранжевая метка, если выбрать нельзя («Running», «In use»), и размер.
// Развёрнуто — путь целиком и полная причина под строкой.
func (u *App) itemRow(it core.Item, expanded bool) fyne.CanvasObject {
	check := widget.NewCheck(it.Name, nil)
	// Ставим состояние ДО назначения OnChanged, чтобы не вызвать обработчик.
	check.Checked = u.sel.Contains(it)
	check.OnChanged = func(on bool) {
		if on {
			if err := u.sel.Add(it); err != nil {
				check.SetChecked(false)
				return
			}
		} else {
			u.sel.Remove(it)
		}
		u.updateBottom()
		u.updateTop()
	}

	ok, why := it.CanSelect()
	if !ok {
		check.Disable()
	}

	right := container.NewHBox()
	if !ok {
		right.Add(pill(u.shortStatus(it), theme.ColorNameWarning))
	}
	right.Add(container.NewCenter(text(it.Size.String(), theme.SizeNameText, colorSecondary, false)))

	if !expanded {
		return container.NewBorder(nil, nil, check, right, pad(0, 0, 6, 6, secondary(it.Detail, false)))
	}

	lines := container.NewVBox(container.NewBorder(nil, nil, check, right))
	indent := func(o fyne.CanvasObject) fyne.CanvasObject { return pad(-10, 0, checkIndent, 0, o) }

	if it.Detail != "" {
		lines.Add(indent(secondary(it.Detail, true)))
	}
	if !ok {
		lines.Add(indent(container.NewBorder(nil, nil,
			pad(6, 0, 0, 0, container.NewVBox(symbol(theme.WarningIcon(), theme.ColorNameWarning, 13))),
			nil,
			colored(u.reason(*why), theme.ColorNameWarning))))
	}
	return lines
}

// shortStatus — короткая метка для компактной строки: почему нельзя выбрать.
func (u *App) shortStatus(it core.Item) string {
	switch {
	case it.Protected != nil:
		return u.t("short.protected")
	case it.Status == core.StatusRunning:
		return u.t("short.running")
	default:
		return u.t("short.in_use")
	}
}

// plural — ключ для числа: для 1 — тот же ключ с окончанием .one,
// для остальных чисел — сам ключ.
// В английском этого хватает; русские тексты написаны так, чтобы
// форма слова от числа не зависела («объектов: 5»).
func plural(key string, n int) string {
	if n == 1 {
		return key + ".one"
	}
	return key
}

// riskPill — цветная «таблетка» риска: зелёная Safe или оранжевая Caution.
func (u *App) riskPill(r core.Risk) fyne.CanvasObject {
	if r == core.RiskCaution {
		return pill(u.t("risk.caution"), theme.ColorNameWarning)
	}
	return pill(u.t("risk.safe"), theme.ColorNameSuccess)
}

// itemsOf — объекты одной категории.
func itemsOf(items []core.Item, categoryID string) []core.Item {
	var out []core.Item
	for _, it := range items {
		if it.CategoryID == categoryID {
			out = append(out, it)
		}
	}
	return out
}

// setText меняет текст надписи в шапке. Очень длинный (например, текст
// ошибки) обрезается: надпись не переносится по строкам.
func setText(t *canvas.Text, s string) {
	const limit = 140
	if r := []rune(s); len(r) > limit { // []rune — по символам, а не по байтам (важно для кириллицы)
		s = string(r[:limit]) + "…"
	}
	if t.Text != s {
		t.Text = s
		t.Refresh()
	}
}
