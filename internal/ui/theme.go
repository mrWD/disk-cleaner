package ui

import (
	"image/color"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Тема в духе приложений Apple: системные цвета macOS (из Human Interface
// Guidelines), шрифт San Francisco, мелкий кегль 13 pt и скругления.
//
// Тема в Fyne — это тип с четырьмя методами: Color, Font, Icon, Size.
// Fyne спрашивает у темы цвет или размер по имени и сам перерисовывает
// всё, когда macOS переключается между светлым и тёмным видом.

// Свои имена цветов и размеров — в дополнение к стандартным Fyne.
// Тема умеет отвечать и на них, поэтому их можно указывать у текста.
const (
	colorSidebar    fyne.ThemeColorName = "dc.sidebar"    // фон боковой панели и нижней полосы
	colorCard       fyne.ThemeColorName = "dc.card"       // фон «сгруппированных» плашек
	colorCardStroke fyne.ThemeColorName = "dc.cardStroke" // тонкая рамка плашек
	colorSecondary  fyne.ThemeColorName = "dc.secondary"  // второстепенный текст (серый)
	colorTrack      fyne.ThemeColorName = "dc.track"      // свободная часть полосы диска
	colorUsed       fyne.ThemeColorName = "dc.used"       // занятая «прочим» часть полосы
	colorCode       fyne.ThemeColorName = "dc.code"       // фон блока с командами

	sizeTitle   fyne.ThemeSizeName = "dc.title"   // крупный заголовок раздела
	sizeCaption fyne.ThemeSizeName = "dc.caption" // мелкие подписи
)

// palette — набор цветов для одного вида (светлого или тёмного).
type palette struct {
	background, sidebar, card, cardStroke, separator color.Color
	label, secondary, tertiary                       color.Color
	accent, green, orange, red                       color.Color
	button, hover, selection, input, scrollbar       color.Color
	overlay, track, used, code                       color.Color
}

// hex превращает "#RRGGBB" или "#RRGGBBAA" в цвет. Ошибок не бывает:
// строки заданы прямо в коде.
func hex(s string) color.NRGBA {
	var c color.NRGBA
	c.A = 0xff
	v := func(i int) uint8 {
		var n uint8
		for _, r := range s[i : i+2] {
			n <<= 4 // сдвиг на 4 бита = умножить на 16
			switch {
			case r >= '0' && r <= '9':
				n |= uint8(r - '0')
			case r >= 'a' && r <= 'f':
				n |= uint8(r - 'a' + 10)
			case r >= 'A' && r <= 'F':
				n |= uint8(r - 'A' + 10)
			}
		}
		return n
	}
	c.R, c.G, c.B = v(1), v(3), v(5)
	if len(s) == 9 {
		c.A = v(7)
	}
	return c
}

// Цвета светлого вида macOS.
var lightPalette = palette{
	background: hex("#F5F5F7"), sidebar: hex("#ECECEF"), card: hex("#FFFFFF"),
	cardStroke: hex("#00000014"), separator: hex("#0000001A"),
	label: hex("#1D1D1F"), secondary: hex("#6E6E73"), tertiary: hex("#AEAEB2"),
	accent: hex("#007AFF"), green: hex("#28A745"), orange: hex("#E8890C"), red: hex("#FF3B30"),
	button: hex("#E5E5EA"), hover: hex("#0000000D"), selection: hex("#0000001A"),
	input: hex("#FFFFFF"), scrollbar: hex("#00000040"), overlay: hex("#FFFFFF"),
	track: hex("#E5E5EA"), used: hex("#AEAEB2"), code: hex("#F2F2F4"),
}

// Цвета тёмного вида macOS.
var darkPalette = palette{
	background: hex("#1C1C1E"), sidebar: hex("#252527"), card: hex("#2C2C2E"),
	cardStroke: hex("#FFFFFF12"), separator: hex("#FFFFFF1A"),
	label: hex("#F5F5F7"), secondary: hex("#98989D"), tertiary: hex("#636366"),
	accent: hex("#0A84FF"), green: hex("#30D158"), orange: hex("#FF9F0A"), red: hex("#FF453A"),
	button: hex("#3A3A3C"), hover: hex("#FFFFFF12"), selection: hex("#FFFFFF1F"),
	input: hex("#1C1C1E"), scrollbar: hex("#FFFFFF40"), overlay: hex("#2C2C2E"),
	track: hex("#3A3A3C"), used: hex("#636366"), code: hex("#1C1C1E"),
}

// Оформление, которое можно выбрать в настройках.
const (
	appearanceSystem = ""      // как в macOS
	appearanceLight  = "light" // всегда светлое
	appearanceDark   = "dark"  // всегда тёмное
)

// applyAppearance выбирает светлый или тёмный вид и заново применяет тему.
// Fyne перерисует все стандартные виджеты (кнопки, поля, галочки),
// а слушатель настроек в New перестроит наши плашки и надписи.
func (u *App) applyAppearance(mode string) {
	switch mode {
	case appearanceLight:
		v := theme.VariantLight
		u.theme.forced = &v
	case appearanceDark:
		v := theme.VariantDark
		u.theme.forced = &v
	default:
		u.theme.forced = nil // вид берётся из macOS
	}
	u.app.Settings().SetTheme(u.theme)
}

// isDark — тёмный ли вид сейчас (с учётом выбора в настройках).
func (u *App) isDark() bool {
	if u.theme.forced != nil {
		return *u.theme.forced == theme.VariantDark
	}
	return u.app.Settings().ThemeVariant() == theme.VariantDark
}

// setAppearance — выбор в настройках: запомнить и сразу применить.
func (u *App) setAppearance(mode string) {
	u.opts.Settings.SetAppearance(mode)
	u.applyAppearance(mode)
}

// appleTheme — наша тема. Шрифты читаются с диска один раз при создании.
type appleTheme struct {
	regular, bold, mono fyne.Resource

	// forced — если не nil, вид всегда этот (нужно для снимков экрана
	// тёмной темы в тестах). В обычной работе вид берётся из macOS.
	forced *fyne.ThemeVariant
}

// newAppleTheme ищет шрифты San Francisco. Если их нет (на другом
// компьютере), остаются шрифты Fyne по умолчанию.
func newAppleTheme() *appleTheme {
	return &appleTheme{
		regular: loadFont("SF-Pro-Text-Regular.otf", "SFNS.ttf"),
		bold:    loadFont("SF-Pro-Text-Semibold.otf"),
		mono:    loadFont("SF-Mono-Regular.otf", "SFNSMono.ttf"),
	}
}

// loadFont возвращает первый найденный из файлов шрифта или nil.
// Шрифты берутся из системы этого Mac, а не кладутся в программу:
// лицензия Apple не разрешает их распространять.
func loadFont(names ...string) fyne.Resource {
	home, _ := os.UserHomeDir()
	dirs := []string{"/Library/Fonts", filepath.Join(home, "Library", "Fonts"), "/System/Library/Fonts"}

	for _, name := range names {
		for _, dir := range dirs {
			if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
				return fyne.NewStaticResource(name, data)
			}
		}
	}
	return nil
}

// palette — цвета для нужного вида.
func (t *appleTheme) palette(v fyne.ThemeVariant) palette {
	if t.forced != nil {
		v = *t.forced
	}
	if v == theme.VariantDark {
		return darkPalette
	}
	return lightPalette
}

// Color — цвет по имени. Всё, что не знаем, отдаём стандартной теме Fyne.
func (t *appleTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	p := t.palette(v)

	switch name {
	case theme.ColorNameBackground:
		return p.background
	case theme.ColorNameForeground:
		return p.label
	case theme.ColorNamePrimary, theme.ColorNameHyperlink, theme.ColorNameFocus:
		return p.accent
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnError,
		theme.ColorNameForegroundOnSuccess, theme.ColorNameForegroundOnWarning:
		return color.White
	case theme.ColorNameButton, theme.ColorNameDisabledButton:
		return p.button
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return p.tertiary
	case theme.ColorNameHover, theme.ColorNamePressed:
		return p.hover
	case theme.ColorNameSelection:
		return p.selection
	case theme.ColorNameSeparator:
		return p.separator
	case theme.ColorNameInputBackground:
		return p.input
	case theme.ColorNameInputBorder:
		return p.cardStroke
	case theme.ColorNameScrollBar:
		return p.scrollbar
	case theme.ColorNameScrollBarBackground:
		return color.Transparent
	case theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground:
		return p.overlay
	case theme.ColorNameHeaderBackground:
		return p.sidebar
	case theme.ColorNameShadow:
		return hex("#00000033")
	case theme.ColorNameSuccess:
		return p.green
	case theme.ColorNameWarning:
		return p.orange
	case theme.ColorNameError:
		return p.red

	case colorSidebar:
		return p.sidebar
	case colorCard:
		return p.card
	case colorCardStroke:
		return p.cardStroke
	case colorSecondary:
		return p.secondary
	case colorTrack:
		return p.track
	case colorUsed:
		return p.used
	case colorCode:
		return p.code
	}

	return theme.DefaultTheme().Color(name, v)
}

// Font — шрифт для стиля текста. Жирный у Apple в интерфейсе — полужирный
// (Semibold), его и берём.
func (t *appleTheme) Font(style fyne.TextStyle) fyne.Resource {
	switch {
	case style.Monospace && t.mono != nil:
		return t.mono
	case style.Bold && !style.Italic && t.bold != nil:
		return t.bold
	case !style.Bold && !style.Italic && !style.Monospace && !style.Symbol && t.regular != nil:
		return t.regular
	}
	return theme.DefaultTheme().Font(style)
}

// Icon — значки берём стандартные.
func (t *appleTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

// Size — размеры: текст 13 pt, как в macOS, и аккуратные скругления.
func (t *appleTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 13
	case theme.SizeNameSubHeadingText:
		return 15
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameInputRadius:
		return 7
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBar:
		return 8
	case theme.SizeNameScrollBarSmall:
		return 4
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNameInlineIcon:
		return 18
	case theme.SizeNameLineSpacing:
		return 3

	case sizeTitle:
		return 26
	case sizeCaption:
		return 12
	}

	return theme.DefaultTheme().Size(name)
}
