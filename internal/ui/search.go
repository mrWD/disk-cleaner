package ui

import (
	"strings"

	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
)

// Search comes in two kinds:
//   - global: the field in the sidebar, searches all sections at once,
//     and the results on the right are grouped by section;
//   - per section: the field above the list of the open section, filters it.
//
// "Select All" acts on what is visible right now: with a search, only
// on what was found, and, as always, only on what can be selected.

// group is a section's category together with its visible items.
type group struct {
	st    *providerState
	cat   core.Category
	items []core.Item
}

// searching reports whether global search is on.
func (u *App) searching() bool {
	return strings.TrimSpace(u.globalQuery) != ""
}

// matches reports whether an item fits the query. The query is split into words, and
// every word must be found somewhere in the name, the path or extra (the name of the
// category or section). Case doesn't matter: "lingary node" will find
// "dual-ai… · node_modules" from ~/Projects/Lingary/….
func matches(it core.Item, query string, extra ...string) bool {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return true
	}

	haystack := strings.ToLower(it.Name + " " + it.Detail + " " + strings.Join(extra, " "))
	for _, w := range words {
		if !strings.Contains(haystack, w) {
			return false
		}
	}
	return true
}

// groupsOf returns the visible categories of one section: those with items that fit
// the query. While the section is scanning or has an error, nothing.
func (u *App) groupsOf(st *providerState, query string) []group {
	if st.scanning || st.err != nil {
		return nil
	}

	var out []group
	for _, cat := range st.provider.Categories() {
		title := u.t(cat.TitleKey())
		section := u.t(st.provider.ID() + ".name")

		var items []core.Item
		for _, it := range itemsOf(st.items, cat.ID) {
			if matches(it, query, title, section) {
				items = append(items, it)
			}
		}
		if len(items) > 0 {
			out = append(out, group{st: st, cat: cat, items: items})
		}
	}
	return out
}

// visibleGroups is everything that is visible on the right right now.
func (u *App) visibleGroups() []group {
	if u.searching() {
		var out []group
		for _, st := range u.states {
			out = append(out, u.groupsOf(st, u.globalQuery)...)
		}
		return out
	}

	if u.current >= len(u.states) {
		return nil
	}
	return u.groupsOf(u.states[u.current], u.sectionQuery)
}

// selectableVisible returns the visible items that can be selected.
func (u *App) selectableVisible() []core.Item {
	var out []core.Item
	for _, g := range u.visibleGroups() {
		for _, it := range g.items {
			if ok, _ := it.CanSelect(); ok {
				out = append(out, it)
			}
		}
	}
	return out
}

// allSelected reports whether all of items are already selected (and whether there are any at all).
func (u *App) allSelected(items []core.Item) bool {
	if len(items) == 0 {
		return false
	}
	for _, it := range items {
		if !u.sel.Contains(it) {
			return false
		}
	}
	return true
}

// toggleVisibleSelection is the "Select All" button in the header: it selects everything
// visible that can be selected, and if everything is already selected, it clears the marks.
func (u *App) toggleVisibleSelection() {
	items := u.selectableVisible()

	if u.allSelected(items) {
		for _, it := range items {
			u.sel.Remove(it)
		}
	} else {
		for _, it := range items {
			_ = u.sel.Add(it) // there can be no error: only what can be selected
		}
	}

	u.refreshDetails()
	u.updateBottom()
}

// setGlobalQuery changes the global query and redraws the list and
// the sidebar. The input field is not recreated, so focus is not lost.
func (u *App) setGlobalQuery(q string) {
	if q == u.globalQuery {
		return
	}
	u.globalQuery = q
	u.detailsScroll = nil // new results start from the top
	u.fillSidebar()
	u.refreshDetails()
}

// setSectionQuery changes the query for the open section.
func (u *App) setSectionQuery(q string) {
	if q == u.sectionQuery {
		return
	}
	u.sectionQuery = q
	u.detailsScroll = nil
	u.refreshDetails()
}

// searchField is a macOS-style search field: a hint inside and a cross
// on the right that appears when something is typed.
func searchField(placeholder, value string, onChange func(string)) *widget.Entry {
	e := widget.NewEntry()
	e.SetPlaceHolder(placeholder)
	e.SetText(value)

	clear := widget.NewButtonWithIcon("", theme.CancelIcon(), func() { e.SetText("") })
	clear.Importance = widget.LowImportance
	if value == "" {
		clear.Hide()
	}
	e.ActionItem = clear

	e.OnChanged = func(s string) {
		if s == "" {
			clear.Hide()
		} else {
			clear.Show()
		}
		onChange(s)
	}
	return e
}
