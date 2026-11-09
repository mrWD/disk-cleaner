package ui

import (
	"strings"

	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
)

// Поиск бывает двух видов:
//   - глобальный — поле в боковой панели, ищет во всех разделах сразу,
//     справа показываются результаты, сгруппированные по разделам;
//   - по разделу — поле над списком открытого раздела, фильтрует его.
//
// «Выбрать все» действует на то, что сейчас видно: с поиском — только
// на найденное, и, как всегда, только на то, что можно выбрать.

// group — категория раздела вместе с её видимыми объектами.
type group struct {
	st    *providerState
	cat   core.Category
	items []core.Item
}

// searching — включён ли глобальный поиск.
func (u *App) searching() bool {
	return strings.TrimSpace(u.globalQuery) != ""
}

// matches — подходит ли объект под запрос. Запрос делится на слова, и
// каждое слово должно найтись где-то в имени, пути или в extra (название
// категории, раздела). Регистр не важен: "lingary node" найдёт
// "dual-ai… · node_modules" из ~/Projects/Lingary/….
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

// groupsOf — видимые категории одного раздела: с объектами, подходящими
// под запрос. Пока раздел сканируется или с ошибкой — ничего.
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

// visibleGroups — всё, что сейчас видно справа.
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

// selectableVisible — видимые объекты, которые можно выбрать.
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

// allSelected — выбраны ли уже все из items (и есть ли они вообще).
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

// toggleVisibleSelection — кнопка «Выбрать все» в шапке: выбирает всё
// видимое, что можно выбрать, а если всё уже выбрано — снимает отметки.
func (u *App) toggleVisibleSelection() {
	items := u.selectableVisible()

	if u.allSelected(items) {
		for _, it := range items {
			u.sel.Remove(it)
		}
	} else {
		for _, it := range items {
			_ = u.sel.Add(it) // ошибки быть не может: только то, что можно выбрать
		}
	}

	u.refreshDetails()
	u.updateBottom()
}

// setGlobalQuery меняет глобальный запрос и перерисовывает список и
// боковую панель. Поле ввода при этом не пересоздаётся — фокус не теряется.
func (u *App) setGlobalQuery(q string) {
	if q == u.globalQuery {
		return
	}
	u.globalQuery = q
	u.detailsScroll = nil // новые результаты — с начала
	u.fillSidebar()
	u.refreshDetails()
}

// setSectionQuery меняет запрос по открытому разделу.
func (u *App) setSectionQuery(q string) {
	if q == u.sectionQuery {
		return
	}
	u.sectionQuery = q
	u.detailsScroll = nil
	u.refreshDetails()
}

// searchField — поле поиска в стиле macOS: подсказка внутри и крестик
// справа, который появляется, когда что-то введено.
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
