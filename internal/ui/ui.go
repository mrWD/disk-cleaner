// Package ui is the application window on Fyne, styled in the spirit of Apple apps.
//
// The UI knows only core: for it every provider is just a core.Provider,
// and they are all shown the same way. The rules (what can be selected, what to
// delete and how) are not invented by the UI; it asks the core.
//
// Files of the package:
//
//	ui.go          - creating the window and the overall layout
//	theme.go       - the theme: macOS colours, the San Francisco font, sizes
//	appearance.go  - the Light / Dark / Auto tiles in Settings
//	widgets.go     - styling building blocks: cards, pills, icons, the disk bar
//	sidebar.go     - the sidebar: search and sections on top; the disk card,
//	                 Rescan and Settings at the bottom
//	disk.go        - the disk card and the global "Clean All Safe" button
//	details.go     - the right-hand part: section header, categories, item rows
//	search.go      - search over all sections and over the open section, "Select All"
//	bottom.go      - the bottom strip: what is selected, Never Clean, Review & Clean…
//	scan.go        - scanning in the background
//	animation.go   - the spinner and skeleton cards shown while a section scans
//	clean.go       - the plan, confirmation and execution; "Clean All Safe"
//	never.go       - the "Never clean" list
//	permissions.go - permissions and the setup window shown before the first scan
//	folders.go     - editable folder lists: project folders, Add Folder…
//	sheet.go       - the window-in-the-window used by Settings and the setup
//	settings.go    - the settings window
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

// Texts of the interface itself (buttons, titles) and of the reasons from core.
//
//go:embed translations
var translations embed.FS

// The app icon for the sidebar.
//
//go:embed assets/appicon.png
var appIcon []byte

// Options is everything the window needs from outside. It is assembled in main.go.
type Options struct {
	Providers []core.Provider
	Executor  core.Executor
	Settings  *settings.Store

	// FindProjectFolders suggests project folders (folders in ~ with git
	// repositories). Optional: without it, the folders are only picked by hand.
	FindProjectFolders func() []string
}

// providerState is what is known about one provider.
// It changes only on the UI thread (in handlers and inside fyne.Do),
// so no mutex is needed.
type providerState struct {
	provider core.Provider
	scanning bool
	gen      int // scan number: results of outdated scans are thrown away
	items    []core.Item
	err      error

	// While scanning: the sidebar spinner, the skeleton cards and their
	// animations, kept across window rebuilds (see animation.go).
	spinner  fyne.CanvasObject
	skeleton fyne.CanvasObject
	anims    []*fyne.Animation
}

// App is the window and its state.
type App struct {
	app    fyne.App
	win    fyne.Window
	opts   Options
	tr     *i18n.Catalog
	theme  *appleTheme
	states []*providerState
	sel    *core.Selection

	current int // which section is selected on the left

	// selView is the view (a section or a search) the current selection was made for.
	// When the view changes, the marks are cleared (see refreshDetails).
	selView string

	// Search and the list view.
	globalQuery  string          // search over all sections (the field in the sidebar)
	sectionQuery string          // search in the open section (the field above the list)
	expanded     map[string]bool // category -> whether it is expanded (collapsed by default)

	// Parts of the window that are rebuilt on changes.
	banner        *fyne.Container // the "Dry run is on" strip above the right-hand part
	disk          *fyne.Container // the disk card in the sidebar
	cleanEvery    *widget.Button  // "Clean All Safe" for all sections (in the disk card)
	sidebarRows   *fyne.Container
	searchEntry   *widget.Entry // the global search field
	details       *fyne.Container
	detailsTop    *fyne.Container   // the header of the right-hand part: title, search, "Select All"
	topKey        string            // which view the header was built for (a section or a search)
	subtitle      *canvas.Text      // the subtitle in the header; it changes without rebuilding the header
	selectAllBtn  *widget.Button    // "Select All / Deselect All" for the visible items
	cleanAllBtn   *widget.Button    // "Clean All Safe" in the open section (there is none in search)
	list          *fyne.Container   // the list goes here, under the header
	detailsScroll *container.Scroll // the scroll of the list (to keep the position)
	bottom        *fyne.Container
	cleanButton   *widget.Button
}

// New creates the window, loads the texts and starts the first scan.
func New(a fyne.App, opts Options) (*App, error) {
	tr := i18n.New()
	if err := tr.Add(translations, "translations"); err != nil {
		return nil, err
	}
	for _, p := range opts.Providers {
		// A provider may have no texts of its own (this happens in tests).
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
	u.applyAppearance(opts.Settings.Appearance()) // light, dark or as in macOS

	u.win = a.NewWindow(u.t("app.title"))
	u.win.SetPadded(false) // the sidebar goes edge to edge, as in macOS
	u.win.Resize(fyne.NewSize(1180, 780))
	u.render()

	// Cmd+F jumps to the search field, as in any Mac app.
	u.win.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: fyne.KeyModifierShortcutDefault},
		func(fyne.Shortcut) { u.win.Canvas().Focus(u.searchEntry) },
	)

	// macOS switched between the light and dark look, so redraw:
	// our cards and texts take their colours at the moment they are created.
	a.Settings().AddListener(func(fyne.Settings) {
		fyne.Do(u.render)
	})

	// First launch (or project folders never confirmed): the setup window
	// comes first, and the scan starts after Continue.
	if opts.Settings.PermissionsAsked() && opts.Settings.RootsConfirmed() {
		u.scanAll()
	} else {
		u.showSetup()
	}
	return u, nil
}

// Run creates the window, shows it and waits until it is closed.
func Run(a fyne.App, opts Options) error {
	u, err := New(a, opts)
	if err != nil {
		return err
	}
	u.win.ShowAndRun()
	return nil
}

// render rebuilds the whole window. It is called at startup and when the
// language, the settings or the light/dark look change. The checkmarks
// are kept, because they live in u.sel.
func (u *App) render() {
	u.win.SetTitle(u.t("app.title"))

	// New theme or language: animations are made again in the new colours.
	for _, st := range u.states {
		st.stopAnimations()
	}

	u.banner = container.NewVBox()
	u.details = container.NewStack() // Stack shows its object over the whole area
	u.detailsScroll = nil
	u.topKey = "" // rebuild the right-hand header (for example, in a new language)
	u.bottom = container.NewStack()

	// On the right: the dry run strip on top (if it is on), the bottom
	// strip at the bottom, the details between them.
	main := edges(u.banner, u.bottom, nil, nil, u.details)

	// The sidebar on the left over the full height, as in Finder and Settings.
	u.win.SetContent(edges(nil, nil, u.buildSidebar(), nil, main))

	u.refresh()
}

// refresh updates everything that depends on the providers' state and on the selection.
func (u *App) refresh() {
	u.fillBanner()
	u.fillDisk()
	u.fillSidebar()
	u.refreshDetails()
	u.updateBottom()
	u.relayout()
}

// relayout lays out the window again. The top and bottom strips change height
// (dry run was turned on, items were selected), and the parent layout doesn't
// find out about that on its own; without this the strips would overlap the content.
func (u *App) relayout() {
	if c := u.win.Content(); c != nil {
		c.Refresh()
	}
}

// look is the section's icon and colour. A provider without core.Look gets grey.
func (u *App) look(p core.Provider) ([]byte, color.Color) {
	if l, ok := p.(core.Look); ok {
		icon, tint := l.Look()
		return icon, hex(tint)
	}
	return nil, hex("#8E8E93")
}

// t is a short way to get a text in the current language.
func (u *App) t(key string, args ...map[string]any) string {
	return u.tr.T(key, args...)
}

// reason is the text of a reason (why something can't be selected or why it is unavailable).
func (u *App) reason(r core.Reason) string {
	return u.tr.T(r.Key, r.Args)
}

// errText is a readable error text.
func (u *App) errText(err error) string {
	var blocked *core.BlockedError
	if errors.As(err, &blocked) {
		return u.t("error.blocked", map[string]any{"Item": blocked.Item.Name, "Reason": u.reason(blocked.Reason)})
	}
	return err.Error()
}

// wrapLabel is a label that wraps long text by words.
func (u *App) wrapLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}

// total is how much can be freed: everything found except what is protected
// (data that is cleaned inside the program itself is not counted; it
// can't be deleted here). What a running program holds is counted: it can
// be deleted once the program is closed.
func total(items []core.Item) core.Bytes {
	var sum core.Bytes
	for _, it := range items {
		if it.Protected == nil {
			sum += it.Size
		}
	}
	return sum
}

// totalAll is the size of everything found, protected data included.
func totalAll(items []core.Item) core.Bytes {
	var sum core.Bytes
	for _, it := range items {
		sum += it.Size
	}
	return sum
}
