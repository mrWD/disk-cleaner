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

// The right-hand part of the window has two parts:
//   - the header (detailsTop): a large title, the search field and "Select All".
//     It is NOT rebuilt on every key press, otherwise the search field
//     would be recreated and lose focus;
//   - the list (list): the category cards. It is rebuilt completely.
//
// By default everything is compact: a category is one header row,
// an item is one row (checkbox, name, path, state label, size).
// A click on a category header expands the explanations and details.

// checkIndent is how far to shift the caption under the checkbox so that it
// starts exactly under the text, not under the little square.
const checkIndent = 31

// viewKey is what is shown on the right now: search results or a section.
func (u *App) viewKey() string {
	if u.searching() {
		return "search"
	}
	return fmt.Sprintf("section:%d", u.current)
}

// refreshDetails updates the right-hand part: the header only if the
// view changed, the list always, keeping the scroll position.
func (u *App) refreshDetails() {
	if u.details == nil || len(u.states) == 0 {
		return
	}

	// The selection belongs to what is visible. If we moved to another section or to
	// a search, the marks are cleared: otherwise the bottom strip would keep
	// items that are not on screen, and it would be easy to delete them by accident.
	// A change of language or appearance does not change the view, so the marks stay.
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

// buildTop builds the header: an icon and a large title, a subtitle, and below
// the section search field and the "Select All" and "Clean All Safe" buttons.
func (u *App) buildTop() *fyne.Container {
	var icon []byte
	tint := theme.Color(colorUsed)
	name := u.t("search.title")
	var search fyne.CanvasObject = gap(0, 0)
	u.cleanAllBtn = nil

	if !u.searching() {
		st := u.states[u.current]
		// "Clean All Safe" means everything safe in this section. The review
		// window with the list of commands still opens first.
		// The label with the size changes after every scan; updateTop keeps it current.
		u.cleanAllBtn = widget.NewButtonWithIcon(u.cleanSafeLabel(st), theme.DeleteIcon(), func() { u.cleanAll(st) })
		icon, tint = u.look(st.provider)
		name = u.t(st.provider.ID() + ".name")
		u.sectionQuery = "" // a new section: search starts from a clean slate
		search = searchField(u.t("search.section", map[string]any{"Section": name}), "", u.setSectionQuery)
	}

	// The title and subtitle are plain canvas.Text labels: they have no
	// inner padding (unlike Label and RichText), so both lines
	// start exactly at the same edge and sit tightly one under the other.
	title := text(name, sizeTitle, theme.ColorNameForeground, true)
	u.subtitle = text("", theme.SizeNameText, colorSecondary, false)

	var mark fyne.CanvasObject
	if icon != nil {
		mark = badge(icon, tint, 48)
	} else {
		mark = container.NewGridWrap(fyne.NewSize(48, 48),
			container.NewStack(roundedRect(tint, 11), pad(11, 11, 11, 11, symbol(theme.SearchIcon(), theme.ColorNameForegroundOnPrimary, 26))))
	}

	// The icon on the left, two lines on the right, centred vertically on the icon:
	// empty "struts" (Spacer) above and below split the free space equally.
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

// updateTop changes what depends on the data in the header: the subtitle,
// the "Select All / Deselect All" label and whether "Clean All Safe" is enabled.
// The search field itself is not touched.
func (u *App) updateTop() {
	if u.subtitle == nil {
		return
	}

	setText(u.subtitle, u.subtitleText())

	if u.cleanAllBtn != nil {
		st := u.states[u.current]
		safe := safeItems(st)

		// A longer label makes the button wider, but the toolbar around it
		// doesn't notice by itself: without relayout the button would overlap
		// Select All.
		if label := u.cleanSafeLabel(st); label != u.cleanAllBtn.Text {
			u.cleanAllBtn.SetText(label)
			u.relayout()
		}
		if len(safe) == 0 {
			u.cleanAllBtn.Disable()
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

// cleanSafeLabel — "Clean All Safe (1.6 GB)" for one section. "(0 B)" explains
// a disabled button by itself: nothing here is Safe and free to remove (for
// example, all of it is in use).
func (u *App) cleanSafeLabel(st *providerState) string {
	if st.scanning {
		return u.t("button.clean_safe.scanning")
	}
	var size core.Bytes
	for _, it := range safeItems(st) {
		size += it.Size
	}
	return u.t("button.clean_safe", map[string]any{"Size": size.String()})
}

// subtitleText is the line under the title.
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

// buildSection builds the list of the open section: the category cards.
func (u *App) buildSection(st *providerState) fyne.CanvasObject {
	if st.scanning {
		return u.scanning(st) // the same object on every rebuild, so it never restarts
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
		// Empty categories go in one line at the bottom: it shows they were checked.
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

// buildSearchResults shows the results of the global search by section:
// the section header with an icon and under it the categories found in it.
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

		// Section header: a click opens the section itself.
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

// buildCategory builds a category card. Collapsed (the default): one
// header row and one row per item. Expanded: plus the explanations
// "What it is. / Why it's safe. / After cleaning." and the full details of the items.
func (u *App) buildCategory(g group) fyne.CanvasObject {
	cat, items := g.cat, g.items
	open := u.expanded[cat.ID]

	// The category checkbox: all selected is a tick, some is a dash, none is empty.
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

	// The rest of the header is clickable: expand or collapse.
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
		// Requirement: next to the buttons, what it is and why it is safe to delete.
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

// explainRow is an explanation row: a small grey icon and text
// "Heading. Explanation", where the heading is a little darker.
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

// itemRow is an item row.
//
// Compact is one line: a checkbox with the name, the path in grey (cut off with "…"),
// an orange label if it can't be selected ("Running", "In use"), and the size.
// Expanded: the full path and the full reason under the row.
func (u *App) itemRow(it core.Item, expanded bool) fyne.CanvasObject {
	check := widget.NewCheck(it.Name, nil)
	// Set the state BEFORE assigning OnChanged, so as not to trigger the handler.
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
	right.Add(u.lockControl(it)) // Never clean on / off, see never.go

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

		// The item itself is on the "Never clean" list: it can come off right here.
		// (For an item inside a listed folder, the folder is changed in Settings.)
		if why.Key == reasonExcluded {
			lines.Add(indent(container.NewHBox(link(u.t("button.allow_clean"), func() { u.allowCleaning(it) }))))
		}
	}
	return lines
}

// shortStatus is a short label for the compact row: why it can't be selected.
func (u *App) shortStatus(it core.Item) string {
	switch {
	case isExclusion(it.Protected):
		return u.t("short.excluded")
	case it.Protected != nil:
		return u.t("short.protected")
	case it.Status == core.StatusRunning:
		return u.t("short.running")
	default:
		return u.t("short.in_use")
	}
}

// plural gives the key for a number: for 1 it is the same key with the suffix .one,
// for other numbers it is the key itself.
// That is enough for English; the Russian texts are written so that
// the word form doesn't depend on the number (like "Items: 5" instead of "5 items").
func plural(key string, n int) string {
	if n == 1 {
		return key + ".one"
	}
	return key
}

// riskPill is a coloured risk "pill": green Safe or orange Caution.
func (u *App) riskPill(r core.Risk) fyne.CanvasObject {
	if r == core.RiskCaution {
		return pill(u.t("risk.caution"), theme.ColorNameWarning)
	}
	return pill(u.t("risk.safe"), theme.ColorNameSuccess)
}

// itemsOf returns the items of one category.
func itemsOf(items []core.Item, categoryID string) []core.Item {
	var out []core.Item
	for _, it := range items {
		if it.CategoryID == categoryID {
			out = append(out, it)
		}
	}
	return out
}

// setText changes the text of a label in the header. A very long one (for example an
// error text) is cut off: the label does not wrap onto several lines.
func setText(t *canvas.Text, s string) {
	const limit = 140
	if r := []rune(s); len(r) > limit { // []rune counts by characters, not bytes (matters for Cyrillic)
		s = string(r[:limit]) + "…"
	}
	if t.Text != s {
		t.Text = s
		t.Refresh()
	}
}
