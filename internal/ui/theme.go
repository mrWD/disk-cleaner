package ui

import (
	"image/color"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// A theme in the spirit of Apple apps: macOS system colours (from the Human Interface
// Guidelines), the San Francisco font, a small 13 pt text size and rounded corners.
//
// A theme in Fyne is a type with four methods: Color, Font, Icon, Size.
// Fyne asks the theme for a colour or a size by name and redraws
// everything itself when macOS switches between light and dark.

// Our own colour and size names, in addition to the standard Fyne ones.
// The theme can answer them too, so they can be used on text.
const (
	colorSidebar    fyne.ThemeColorName = "dc.sidebar"    // background of the sidebar and the bottom strip
	colorCard       fyne.ThemeColorName = "dc.card"       // background of the "grouped" cards
	colorCardStroke fyne.ThemeColorName = "dc.cardStroke" // thin border of the cards
	colorSecondary  fyne.ThemeColorName = "dc.secondary"  // secondary text (grey)
	colorTrack      fyne.ThemeColorName = "dc.track"      // the free part of the disk bar
	colorUsed       fyne.ThemeColorName = "dc.used"       // the part of the bar used by "other" data
	colorCode       fyne.ThemeColorName = "dc.code"       // background of the block with commands

	sizeTitle   fyne.ThemeSizeName = "dc.title"   // large section title
	sizeCaption fyne.ThemeSizeName = "dc.caption" // small captions
)

// palette is a set of colours for one appearance (light or dark).
type palette struct {
	background, sidebar, card, cardStroke, separator color.Color
	label, secondary, tertiary                       color.Color
	accent, green, orange, red                       color.Color
	button, hover, selection, input, scrollbar       color.Color
	overlay, track, used, code                       color.Color
}

// hex turns "#RRGGBB" or "#RRGGBBAA" into a colour. There can be no errors:
// the strings are set right in the code.
func hex(s string) color.NRGBA {
	var c color.NRGBA
	c.A = 0xff
	v := func(i int) uint8 {
		var n uint8
		for _, r := range s[i : i+2] {
			n <<= 4 // shifting by 4 bits = multiplying by 16
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

// Colours of the macOS light appearance.
var lightPalette = palette{
	background: hex("#F5F5F7"), sidebar: hex("#ECECEF"), card: hex("#FFFFFF"),
	cardStroke: hex("#00000014"), separator: hex("#0000001A"),
	label: hex("#1D1D1F"), secondary: hex("#6E6E73"), tertiary: hex("#AEAEB2"),
	accent: hex("#007AFF"), green: hex("#28A745"), orange: hex("#E8890C"), red: hex("#FF3B30"),
	button: hex("#E5E5EA"), hover: hex("#0000000D"), selection: hex("#0000001A"),
	input: hex("#FFFFFF"), scrollbar: hex("#00000040"), overlay: hex("#FFFFFF"),
	track: hex("#E5E5EA"), used: hex("#AEAEB2"), code: hex("#F2F2F4"),
}

// Colours of the macOS dark appearance.
var darkPalette = palette{
	background: hex("#1C1C1E"), sidebar: hex("#252527"), card: hex("#2C2C2E"),
	cardStroke: hex("#FFFFFF12"), separator: hex("#FFFFFF1A"),
	label: hex("#F5F5F7"), secondary: hex("#98989D"), tertiary: hex("#636366"),
	accent: hex("#0A84FF"), green: hex("#30D158"), orange: hex("#FF9F0A"), red: hex("#FF453A"),
	button: hex("#3A3A3C"), hover: hex("#FFFFFF12"), selection: hex("#FFFFFF1F"),
	input: hex("#1C1C1E"), scrollbar: hex("#FFFFFF40"), overlay: hex("#2C2C2E"),
	track: hex("#3A3A3C"), used: hex("#636366"), code: hex("#1C1C1E"),
}

// The appearance options that can be chosen in settings.
const (
	appearanceSystem = ""      // same as macOS
	appearanceLight  = "light" // always light
	appearanceDark   = "dark"  // always dark
)

// applyAppearance picks the light or dark look and applies the theme again.
// Fyne will redraw all standard widgets (buttons, fields, checkboxes),
// and the settings listener in New will rebuild our cards and labels.
func (u *App) applyAppearance(mode string) {
	switch mode {
	case appearanceLight:
		v := theme.VariantLight
		u.theme.forced = &v
	case appearanceDark:
		v := theme.VariantDark
		u.theme.forced = &v
	default:
		u.theme.forced = nil // the look is taken from macOS
	}
	u.app.Settings().SetTheme(u.theme)
}

// isDark reports whether the look is dark right now (taking the choice in settings into account).
func (u *App) isDark() bool {
	if u.theme.forced != nil {
		return *u.theme.forced == theme.VariantDark
	}
	return u.app.Settings().ThemeVariant() == theme.VariantDark
}

// setAppearance handles the choice in settings: remember it and apply it at once.
func (u *App) setAppearance(mode string) {
	u.opts.Settings.SetAppearance(mode)
	u.applyAppearance(mode)
}

// appleTheme is our theme. Fonts are read from disk once, when it is created.
type appleTheme struct {
	regular, bold, mono fyne.Resource

	// forced, if not nil, fixes the look to this value (needed for screenshots
	// of the dark theme in tests). In normal use the look is taken from macOS.
	forced *fyne.ThemeVariant
}

// newAppleTheme looks for the San Francisco fonts. If they are missing (on another
// computer), the default Fyne fonts stay.
func newAppleTheme() *appleTheme {
	return &appleTheme{
		regular: loadFont("SF-Pro-Text-Regular.otf", "SFNS.ttf"),
		bold:    loadFont("SF-Pro-Text-Semibold.otf"),
		mono:    loadFont("SF-Mono-Regular.otf", "SFNSMono.ttf"),
	}
}

// loadFont returns the first one found among the font files, or nil.
// The fonts are taken from this Mac's system and are not shipped with the program:
// Apple's licence does not allow distributing them.
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

// palette returns the colours for the needed appearance.
func (t *appleTheme) palette(v fyne.ThemeVariant) palette {
	if t.forced != nil {
		v = *t.forced
	}
	if v == theme.VariantDark {
		return darkPalette
	}
	return lightPalette
}

// Color returns a colour by name. Anything we don't know goes to the standard Fyne theme.
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

// Font returns the font for a text style. Bold in Apple's interface is semibold
// (Semibold), so that is what we take.
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

// Icon: we use the standard icons.
func (t *appleTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

// Size: text is 13 pt, as in macOS, with neat rounded corners.
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
