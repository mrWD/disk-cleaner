// Package ui — окно приложения на Fyne, оформленное в духе приложений Apple.
//
// UI знает только core: каждый провайдер для него — просто core.Provider,
// и показываются они одинаково. Правила (что можно выбрать, что и как
// удалить) UI не придумывает, а спрашивает у ядра.
//
// Файлы пакета:
//
//	ui.go        — создание окна и общая раскладка
//	theme.go     — тема: цвета macOS, шрифт San Francisco, размеры
//	widgets.go   — кирпичики оформления: плашки, таблетки, значки, полоса диска
//	sidebar.go   — боковая панель: диск, «Очистить всё», поиск, разделы, кнопки
//	disk.go      — плашка диска в боковой панели и кнопка «Очистить всё»
//	details.go   — правая часть: шапка раздела, категории, строки объектов
//	search.go    — поиск по всем разделам и по открытому разделу, «Выбрать все»
//	bottom.go    — нижняя полоса: что выбрано и кнопка очистки
//	scan.go      — сканирование в фоне
//	clean.go     — план, подтверждение и выполнение; «Очистить всё»
//	settings.go  — окно настроек
package ui

import (
	"embed"
	"errors"
	"fmt"
	"image/color"
	"io/fs"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/i18n"
	"disk-cleaner/internal/settings"
)

// Тексты самого интерфейса (кнопки, заголовки) и причин из core.
//
//go:embed translations
var translations embed.FS

// Значок приложения для боковой панели.
//
//go:embed assets/appicon.png
var appIcon []byte

// Options — всё, что окну нужно снаружи. Собирается в main.go.
type Options struct {
	Providers []core.Provider
	Executor  core.Executor
	Settings  *settings.Store
}

// providerState — что известно об одном провайдере.
// Меняется только в UI-потоке (в обработчиках и внутри fyne.Do),
// поэтому мьютекс не нужен.
type providerState struct {
	provider core.Provider
	scanning bool
	gen      int // номер сканирования: результаты устаревших сканирований отбрасываем
	items    []core.Item
	err      error
}

// App — окно и его состояние.
type App struct {
	app    fyne.App
	win    fyne.Window
	opts   Options
	tr     *i18n.Catalog
	theme  *appleTheme
	states []*providerState
	sel    *core.Selection

	current int // какой раздел выбран слева

	// selView — для какого вида (раздел или поиск) сделан текущий выбор.
	// Сменился вид — отметки снимаются (см. refreshDetails).
	selView string

	// Поиск и вид списка.
	globalQuery  string          // поиск по всем разделам (поле в боковой панели)
	sectionQuery string          // поиск в открытом разделе (поле над списком)
	expanded     map[string]bool // категория → развёрнута ли (по умолчанию — свёрнута)

	// Части окна, которые перестраиваются при изменениях.
	banner        *fyne.Container // полоса «Включён пробный режим» над правой частью
	disk          *fyne.Container // плашка диска в боковой панели
	cleanEvery    *widget.Button  // «Очистить всё» во всех разделах (в плашке диска)
	sidebarRows   *fyne.Container
	searchEntry   *widget.Entry // поле глобального поиска
	details       *fyne.Container
	detailsTop    *fyne.Container   // шапка правой части: заголовок, поиск, «Выбрать все»
	topKey        string            // для какого вида построена шапка (раздел или поиск)
	subtitle      *canvas.Text      // подзаголовок в шапке — меняется без перестройки шапки
	selectAllBtn  *widget.Button    // «Выбрать все / Снять все» для видимых объектов
	cleanAllBtn   *widget.Button    // «Очистить всё» в открытом разделе (в поиске его нет)
	list          *fyne.Container   // сюда кладётся список под шапкой
	detailsScroll *container.Scroll // прокрутка списка (чтобы сохранять позицию)
	bottom        *fyne.Container
	cleanButton   *widget.Button
}

// New создаёт окно, загружает тексты и запускает первое сканирование.
func New(a fyne.App, opts Options) (*App, error) {
	tr := i18n.New()
	if err := tr.Add(translations, "translations"); err != nil {
		return nil, err
	}
	for _, p := range opts.Providers {
		// У провайдера может не быть своих текстов (так бывает в тестах).
		if err := tr.Add(p.Translations(), "translations"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s translations: %w", p.ID(), err)
		}
	}
	tr.SetLanguage(opts.Settings.Language())

	u := &App{
		opts:     opts,
		tr:       tr,
		sel:      core.NewSelection(),
		theme:    newAppleTheme(),
		expanded: map[string]bool{},
	}
	for _, p := range opts.Providers {
		u.states = append(u.states, &providerState{provider: p})
	}

	u.app = a
	u.applyAppearance(opts.Settings.Appearance()) // светлое, тёмное или как в macOS

	u.win = a.NewWindow(u.t("app.title"))
	u.win.SetPadded(false) // боковая панель — от края до края, как в macOS
	u.win.Resize(fyne.NewSize(1180, 780))
	u.render()

	// ⌘F — перейти в поле поиска, как в любом приложении Mac.
	u.win.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: fyne.KeyModifierShortcutDefault},
		func(fyne.Shortcut) { u.win.Canvas().Focus(u.searchEntry) },
	)

	// macOS переключилась между светлым и тёмным видом — перерисовать:
	// наши плашки и тексты берут цвета в момент создания.
	a.Settings().AddListener(func(fyne.Settings) {
		fyne.Do(u.render)
	})

	u.scanAll()
	return u, nil
}

// Run создаёт окно, показывает его и ждёт, пока его закроют.
func Run(a fyne.App, opts Options) error {
	u, err := New(a, opts)
	if err != nil {
		return err
	}
	u.win.ShowAndRun()
	return nil
}

// render строит всё окно заново. Вызывается при запуске, при смене
// языка, настроек или светлого/тёмного вида. Галочки при этом
// сохраняются — они живут в u.sel.
func (u *App) render() {
	u.win.SetTitle(u.t("app.title"))

	u.banner = container.NewVBox()
	u.details = container.NewStack() // Stack показывает свой объект на всю площадь
	u.detailsScroll = nil
	u.topKey = "" // шапку правой части — построить заново (например, на новом языке)
	u.bottom = container.NewStack()

	// Справа: полоса пробного режима сверху (если он включён), нижняя
	// полоса снизу, подробности между ними.
	main := edges(u.banner, u.bottom, nil, nil, u.details)

	// Боковая панель слева на всю высоту, как в Finder и Настройках.
	u.win.SetContent(edges(nil, nil, u.buildSidebar(), nil, main))

	u.refresh()
}

// refresh обновляет всё, что зависит от состояния провайдеров и выбора.
func (u *App) refresh() {
	u.fillBanner()
	u.fillDisk()
	u.fillSidebar()
	u.refreshDetails()
	u.updateBottom()
	u.relayout()
}

// relayout заново раскладывает окно. Верхняя и нижняя полосы меняют высоту
// (включили пробный режим, выбрали объекты), а родительская раскладка сама
// об этом не узнаёт — без этого полосы налезали бы на содержимое.
func (u *App) relayout() {
	if c := u.win.Content(); c != nil {
		c.Refresh()
	}
}

// look — значок и цвет раздела. Провайдер без core.Look получает серый.
func (u *App) look(p core.Provider) ([]byte, color.Color) {
	if l, ok := p.(core.Look); ok {
		icon, tint := l.Look()
		return icon, hex(tint)
	}
	return nil, hex("#8E8E93")
}

// t — короткий способ взять текст на текущем языке.
func (u *App) t(key string, args ...map[string]any) string {
	return u.tr.T(key, args...)
}

// reason — текст причины (почему нельзя выбрать или почему недоступно).
func (u *App) reason(r core.Reason) string {
	return u.tr.T(r.Key, r.Args)
}

// errText — понятный текст ошибки.
func (u *App) errText(err error) string {
	var blocked *core.BlockedError
	if errors.As(err, &blocked) {
		return u.t("error.blocked", map[string]any{"Item": blocked.Item.Name, "Reason": u.reason(blocked.Reason)})
	}
	return err.Error()
}

// wrapLabel — надпись, которая переносит длинный текст по словам.
func (u *App) wrapLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}

// total — сколько можно освободить: всё найденное, кроме защищённого
// (данные, которые чистят в самой программе, сюда не входят — здесь их
// удалить нельзя). Занятое запущенной программой — входит: его можно
// будет удалить, когда программу закроют.
func total(items []core.Item) core.Bytes {
	var sum core.Bytes
	for _, it := range items {
		if it.Protected == nil {
			sum += it.Size
		}
	}
	return sum
}

// totalAll — размер всего найденного, вместе с защищённым.
func totalAll(items []core.Item) core.Bytes {
	var sum core.Bytes
	for _, it := range items {
		sum += it.Size
	}
	return sum
}
