package ui

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/i18n"
	"disk-cleaner/internal/settings"
)

// keyRe находит в коде строки, похожие на ключи переводов: "button.rescan".
// Пути импорта ("fyne.io/fyne/v2") не подходят: в них есть / и -.
var keyRe = regexp.MustCompile(`"([a-z][a-z_]*(?:\.[a-z_]+)+)"`)

// pluralRe находит ключи, у которых есть форма для единицы: plural("ключ", n).
var pluralRe = regexp.MustCompile(`plural\("([a-z_.]+)"`)

// notTextKey — имена файлов, которые keyRe тоже находит.
var notTextKey = regexp.MustCompile(`\.(svg|png|json|otf|ttf)$`)

// TestEveryKeyIsTranslated — каждый ключ, который встречается в коде UI,
// есть во всех языках. Ловит опечатки вроде "core.reasong.in_use".
func TestEveryKeyIsTranslated(t *testing.T) {
	catalog := i18n.NewWithSystem(nil)
	if err := catalog.Add(translations, "translations"); err != nil {
		t.Fatal(err)
	}

	keys := []string{core.ReasonRunning, core.ReasonInUse}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// Для plural("ключ", n) нужен ещё и вариант "ключ.one".
		for _, m := range pluralRe.FindAllStringSubmatch(string(src), -1) {
			keys = append(keys, m[1]+".one")
		}
		for _, m := range keyRe.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			// Не ключи перевода: свои имена цветов и размеров темы ("dc.card")
			// и имена файлов ("icon.svg").
			if strings.HasPrefix(key, "dc.") || notTextKey.MatchString(key) {
				continue
			}
			keys = append(keys, key)
		}
	}

	for _, lang := range catalog.Languages() {
		for _, k := range keys {
			if !catalog.Has(lang, k) {
				t.Errorf("%s.json: missing %q", lang, k)
			}
		}
		for _, k := range catalog.Keys(lang) {
			if !catalog.Has(i18n.Fallback, k) {
				t.Errorf("%s.json: %q is not in en.json", lang, k)
			}
		}
	}
}

// fakeProvider — провайдер для проверки окна. Его сканирование в фоне
// никогда не заканчивается: результат тест подставляет сам (loaded),
// чтобы фоновые горутины не мешали проверкам.
type fakeProvider struct {
	id    string
	items []core.Item
}

func (f fakeProvider) ID() string { return f.id }
func (f fakeProvider) Categories() []core.Category {
	return []core.Category{{ID: f.id + ".cache", Risk: core.RiskSafe}, {ID: f.id + ".empty", Risk: core.RiskCaution}}
}
func (fakeProvider) Scan(ctx context.Context) ([]core.Item, error) {
	<-ctx.Done() // ждать вечно: контекст фоновый и не отменяется
	return nil, ctx.Err()
}
func (fakeProvider) Plan(items []core.Item) ([]core.Action, error) {
	return []core.Action{{RemovePath: "/tmp/x"}}, nil
}
func (fakeProvider) Translations() embed.FS { return embed.FS{} }

// texts собирает все тексты, видимые в дереве виджетов.
func texts(obj fyne.CanvasObject) []string {
	var out []string
	switch o := obj.(type) { // switch по типу значения
	case *widget.Label:
		out = append(out, o.Text)
	case *canvas.Text:
		out = append(out, o.Text)
	case *widget.Check:
		out = append(out, o.Text)
	case *widget.RichText:
		out = append(out, o.String())
	case *widget.Button:
		out = append(out, o.Text)
	case *tappable:
		out = append(out, texts(o.content)...)
	case *container.Scroll:
		out = append(out, texts(o.Content)...)
	case *fyne.Container:
		for _, child := range o.Objects {
			out = append(out, texts(child)...)
		}
	}
	return out
}

// Объекты для сценариев: два раздела, «xcode» и «docker».
var (
	derived  = core.Item{ID: "d1", CategoryID: "xcode.cache", Name: "Lingary", Detail: "~/Library/Developer/Xcode/DerivedData/Lingary-abc", Size: 2_000_000}
	archive  = core.Item{ID: "d2", CategoryID: "xcode.cache", Name: "SpesaPlan 1.0.0", Detail: "~/Library/Developer/Xcode/Archives/SpesaPlan", Size: 1_000_000}
	redis    = core.Item{ID: "c1", CategoryID: "docker.cache", Name: "redis · redis:8", Status: core.StatusRunning, Size: 1_000}
	postgres = core.Item{ID: "c2", CategoryID: "docker.cache", Name: "postgres:17", Detail: "Lingary database", Size: 3_000_000}
)

// loaded — окно с двумя разделами, у которых уже есть результаты сканирования.
func loaded(t *testing.T) *App {
	t.Helper()

	a := test.NewTempApp(t) // приложение без настоящего экрана — для тестов
	store := settings.New(a.Preferences(), t.TempDir())
	store.SetLanguage("en")

	u, err := New(a, Options{
		Providers: []core.Provider{
			fakeProvider{id: "xcode"},
			fakeProvider{id: "docker"},
		},
		Settings: store,
	})
	if err != nil {
		t.Fatal(err)
	}

	results := [][]core.Item{{derived, archive}, {redis, postgres}}
	for i, st := range u.states {
		st.scanning, st.items, st.err = false, results[i], nil
	}
	u.refresh()
	return u
}

// shown — все тексты правой части одной строкой.
func shown(u *App) string {
	return strings.Join(texts(u.details), "\n")
}

func mustShow(t *testing.T, u *App, want ...string) {
	t.Helper()
	all := shown(u)
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("window does not show %q.\nShown:\n%s", w, all)
		}
	}
}

func mustNotShow(t *testing.T, u *App, unwanted ...string) {
	t.Helper()
	all := shown(u)
	for _, w := range unwanted {
		if strings.Contains(all, w) {
			t.Errorf("window should not show %q.\nShown:\n%s", w, all)
		}
	}
}

func TestCompactView(t *testing.T) {
	t.Run("by default each item is one line with name, path, short status and size", func(t *testing.T) {
		// Given: открыт раздел docker
		u := loaded(t)
		u.selectProvider(1)

		// Then: имя, путь, короткая метка и размер видны; длинных объяснений нет
		mustShow(t, u, "redis · redis:8", "Running", "1.0 kB", "postgres:17", "Lingary database", "2 items")
		mustNotShow(t, u, "What it is.", "Running right now.")
	})

	t.Run("expanding a category shows the explanation and the full reason", func(t *testing.T) {
		// Given: открыт раздел docker
		u := loaded(t)
		u.selectProvider(1)

		// When: раскрываем категорию
		u.expanded["docker.cache"] = true
		u.refreshDetails()

		// Then: видны объяснения и полная причина блокировки
		mustShow(t, u, "What it is.", "Why it’s safe.", "After cleaning.", "Running right now.")
	})

	t.Run("nothing is selected at start", func(t *testing.T) {
		u := loaded(t)
		if u.sel.Len() != 0 || !u.cleanButton.Disabled() {
			t.Error("nothing should be selected and the clean button should be disabled")
		}
	})
}

func TestSectionSearch(t *testing.T) {
	t.Run("search in a section keeps only matching items", func(t *testing.T) {
		// Given: открыт раздел xcode
		u := loaded(t)

		// When: ищем по пути
		u.setSectionQuery("archives")

		// Then: остался только архив
		mustShow(t, u, "SpesaPlan 1.0.0")
		mustNotShow(t, u, "Lingary")
	})

	t.Run("all words of the query must match", func(t *testing.T) {
		u := loaded(t)

		u.setSectionQuery("lingary derived")
		mustShow(t, u, "Lingary")

		u.setSectionQuery("lingary archives")
		mustShow(t, u, "Nothing matches")
	})

	t.Run("switching sections clears the section search", func(t *testing.T) {
		u := loaded(t)
		u.setSectionQuery("archives")

		u.selectProvider(1)

		if u.sectionQuery != "" {
			t.Errorf("sectionQuery = %q, want empty after switching", u.sectionQuery)
		}
		mustShow(t, u, "postgres:17", "redis · redis:8")
	})
}

func TestGlobalSearch(t *testing.T) {
	t.Run("global search finds items in every section", func(t *testing.T) {
		u := loaded(t)

		// When: ищем слово, которое есть и в Xcode, и в Docker
		u.setGlobalQuery("lingary")

		// Then: результаты из обоих разделов, с заголовками разделов
		mustShow(t, u, "Lingary", "postgres:17", "xcode.name", "docker.name", "2 matches")
		mustNotShow(t, u, "SpesaPlan", "redis")
	})

	t.Run("opening a section from the results leaves the search", func(t *testing.T) {
		u := loaded(t)
		u.setGlobalQuery("lingary")

		u.openSection(1)

		if u.searching() || u.current != 1 {
			t.Errorf("searching = %v, current = %d; want section 1 without search", u.searching(), u.current)
		}
	})
}

func TestSelectAll(t *testing.T) {
	t.Run("select all picks only visible items that can be selected", func(t *testing.T) {
		// Given: глобальный поиск показывает postgres и Lingary, а redis запущен
		u := loaded(t)
		u.setGlobalQuery("lingary")

		// When: «Выбрать все»
		u.toggleVisibleSelection()

		// Then: выбраны оба найденных, но не архив (его не видно) и не redis
		if !u.sel.Contains(postgres) || !u.sel.Contains(derived) || u.sel.Contains(archive) || u.sel.Contains(redis) {
			t.Errorf("selection = %v", u.sel.Items())
		}
		if u.selectAllBtn.Text != "Deselect All" {
			t.Errorf("button = %q, want Deselect All after selecting everything", u.selectAllBtn.Text)
		}
	})

	t.Run("pressing it again deselects them", func(t *testing.T) {
		u := loaded(t)
		u.toggleVisibleSelection()
		u.toggleVisibleSelection()

		if u.sel.Len() != 0 {
			t.Errorf("selection = %v, want empty", u.sel.Items())
		}
	})

	t.Run("running items are never selected", func(t *testing.T) {
		u := loaded(t)
		u.selectProvider(1)

		u.toggleVisibleSelection()

		if u.sel.Contains(redis) || !u.sel.Contains(postgres) {
			t.Errorf("selection = %v", u.sel.Items())
		}
	})
}

func TestAppearance(t *testing.T) {
	t.Run("dark appearance chosen in settings wins over light macOS", func(t *testing.T) {
		// Given: macOS в светлом виде (тестовое приложение — светлое)
		u := loaded(t)

		// When: выбираем тёмное оформление
		u.applyAppearance(appearanceDark)

		// Then: тема отдаёт тёмные цвета даже на вопрос о светлом виде
		if got := u.theme.Color(theme.ColorNameBackground, theme.VariantLight); got != darkPalette.background {
			t.Errorf("background = %v, want dark %v", got, darkPalette.background)
		}
	})

	t.Run("settings offer three options and remember the choice", func(t *testing.T) {
		// Given: сохранено «Авто»
		u := loaded(t)
		picker := u.newAppearancePicker(appearanceSystem)

		// Then: три плитки — Светлое, Тёмное, Авто
		if got := strings.Join(texts(picker.box), ","); got != "Light,Dark,Auto" {
			t.Errorf("tiles = %q, want Light,Dark,Auto", got)
		}

		// When: выбираем «Тёмное» и сохраняем
		picker.pick(appearanceDark)
		u.setAppearance(picker.selected)

		// Then: тёмный вид включён и запомнен
		if !u.isDark() || u.opts.Settings.Appearance() != appearanceDark {
			t.Errorf("dark = %v, saved = %q", u.isDark(), u.opts.Settings.Appearance())
		}
	})

	t.Run("same as macOS follows the system", func(t *testing.T) {
		u := loaded(t)
		u.applyAppearance(appearanceSystem)
		if u.theme.forced != nil {
			t.Error("system appearance must not force a variant")
		}
	})
}

func TestSwitchingViewClearsSelection(t *testing.T) {
	t.Run("opening another section clears the checkboxes", func(t *testing.T) {
		// Given: в разделе xcode отмечен Lingary
		u := loaded(t)
		_ = u.sel.Add(derived)
		u.updateBottom()

		// When: открыли раздел docker
		u.selectProvider(1)

		// Then: ничего не выбрано, и нижняя полоса это показывает
		if u.sel.Len() != 0 {
			t.Errorf("selection = %v, want empty after switching sections", u.sel.Items())
		}
		if bottom := strings.Join(texts(u.bottom), "\n"); !strings.Contains(bottom, "Nothing selected") {
			t.Errorf("bottom bar should say Nothing selected, shows:\n%s", bottom)
		}
	})

	t.Run("starting a global search clears them too", func(t *testing.T) {
		u := loaded(t)
		_ = u.sel.Add(derived)

		u.setGlobalQuery("postgres")

		if u.sel.Len() != 0 {
			t.Errorf("selection = %v, want empty after starting a search", u.sel.Items())
		}
	})

	t.Run("staying in the section keeps them", func(t *testing.T) {
		// Given: отмечен Lingary
		u := loaded(t)
		_ = u.sel.Add(derived)

		// When: раскрыли категорию, сменили оформление (окно перестроилось)
		// и ещё раз открыли тот же раздел
		u.expanded["xcode.cache"] = true
		u.refreshDetails()
		u.setAppearance(appearanceDark)
		u.render()
		u.selectProvider(0)

		// Then: отметка на месте
		if !u.sel.Contains(derived) {
			t.Errorf("selection = %v, want Lingary kept", u.sel.Items())
		}
	})
}

func TestCleanAll(t *testing.T) {
	// withCaution — тот же набор плюс объект в категории «Осторожно».
	emulator := core.Item{ID: "e1", CategoryID: "xcode.empty", Name: "Old emulator", Size: 5_000_000}
	withCaution := func(t *testing.T) *App {
		u := loaded(t)
		u.states[0].items = append(u.states[0].items, emulator)
		u.refresh()
		return u
	}

	t.Run("section button takes only safe items that can be selected", func(t *testing.T) {
		u := withCaution(t)

		sel := u.safeSelection(u.states[0])

		if !sel.Contains(derived) || !sel.Contains(archive) || sel.Contains(emulator) || sel.Len() != 2 {
			t.Errorf("safe selection = %v, want Lingary and SpesaPlan only", sel.Items())
		}
	})

	t.Run("global button covers every section but skips running items", func(t *testing.T) {
		u := withCaution(t)

		sel := u.safeSelection(u.states...)

		if !sel.Contains(postgres) || sel.Contains(redis) || sel.Contains(emulator) || sel.Len() != 3 {
			t.Errorf("safe selection = %v, want Lingary, SpesaPlan and postgres", sel.Items())
		}
	})

	t.Run("it does not touch the checkboxes on screen", func(t *testing.T) {
		u := loaded(t)

		u.cleanAll(u.states...) // откроется окно проверки; «Отмена» ничего не изменит

		if u.sel.Len() != 0 {
			t.Errorf("selection = %v, want untouched", u.sel.Items())
		}
	})

	t.Run("buttons are off while scanning and when nothing is safe", func(t *testing.T) {
		u := loaded(t)
		if u.cleanEvery.Disabled() || u.cleanAllBtn.Disabled() {
			t.Fatal("both Clean All buttons should be on when safe items are found")
		}

		// When: раздел docker снова сканируется
		u.states[1].scanning = true
		u.refresh()

		// Then: общая кнопка ждёт конца сканирования, а кнопка раздела xcode — нет
		if !u.cleanEvery.Disabled() {
			t.Error("global Clean All should wait until scanning finishes")
		}
		if u.cleanAllBtn.Disabled() {
			t.Error("Clean All in xcode should stay on: xcode is not scanning")
		}

		// When: в xcode осталось только «Осторожно»
		u.states[0].items = []core.Item{emulator}
		u.refresh()

		// Then: кнопка раздела выключена
		if !u.cleanAllBtn.Disabled() {
			t.Error("Clean All in xcode should be off: nothing safe there")
		}
	})
}
