//go:build screenshots

// A tool, not an ordinary test: screenshots of all the app's screens on this Mac's
// real data, without opening a window (Fyne can draw "in memory").
// The ordinary go test skips it because of the go:build line above.
//
// Run from the project root (the images appear in the screenshots/ folder):
//
//	go test -tags screenshots -run TestScreenshots ./internal/ui/ -args -lang=ru -dark
//
// The test imports the providers, which is only allowed in tests: the ui package
// itself knows nothing about providers.
package ui

import (
	"context"
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/executor"
	"disk-cleaner/internal/providers/android"
	"disk-cleaner/internal/providers/appcaches"
	"disk-cleaner/internal/providers/devtools"
	"disk-cleaner/internal/providers/docker"
	"disk-cleaner/internal/providers/downloads"
	"disk-cleaner/internal/providers/gitscan"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/providers/models"
	"disk-cleaner/internal/providers/packagecaches"
	"disk-cleaner/internal/providers/projects"
	"disk-cleaner/internal/providers/simulators"
	"disk-cleaner/internal/providers/worktrees"
	"disk-cleaner/internal/providers/xcode"
	"disk-cleaner/internal/settings"
	"disk-cleaner/internal/update"
)

var (
	shotLang = flag.String("lang", "en", "interface language: en or ru")
	shotDir  = flag.String("out", "../../screenshots", "where to save the images")
	shotDark = flag.Bool("dark", false, "take the screenshots in the dark look")
)

// frozen is a provider whose background scan never finishes.
// We get the results beforehand and put them in ourselves: this way background goroutines
// don't get in the way of taking the screenshots.
type frozen struct{ core.Provider }

// Look passes on the icon and colour of the real provider: the embedded interface
// gives only the core.Provider methods, and Look is not one of them.
func (f frozen) Look() ([]byte, string) {
	if l, ok := f.Provider.(core.Look); ok {
		return l.Look()
	}
	return nil, "#8E8E93"
}

func (frozen) Scan(ctx context.Context) ([]core.Item, error) {
	<-ctx.Done() // wait forever (the context is a background one and is never cancelled)
	return nil, ctx.Err()
}

func TestScreenshots(t *testing.T) {
	if err := os.MkdirAll(*shotDir, 0o755); err != nil {
		t.Fatal(err)
	}

	a := test.NewTempApp(t)
	store := settings.New(a.Preferences(), kit.Home())
	store.SetLanguage(*shotLang)
	store.SetPermissionsAsked(true) // the setup window is captured separately at the end
	store.SetRoots([]string{filepath.Join(kit.Home(), "Projects")})
	ex, _ := executor.New()

	real := []core.Provider{
		worktrees.New(store.Roots), projects.New(store.Roots), xcode.New(), simulators.New(),
		android.New(), docker.New(), models.New(), devtools.New(),
		packagecaches.New(), appcaches.New(), downloads.New(store.UseDownloads),
	}

	// Real scanning, beforehand and one provider after another.
	type result struct {
		items []core.Item
		err   error
	}
	results := make([]result, len(real))
	wrapped := make([]core.Provider, len(real))
	for i, p := range real {
		items, err := p.Scan(context.Background())
		results[i] = result{items, err}
		wrapped[i] = frozen{p}
	}

	u, err := New(a, Options{Providers: wrapped, Executor: ex, Settings: store,
		FindProjectFolders: func() []string { return gitscan.ProjectFolders(kit.Home()) }})
	if err != nil {
		t.Fatal(err)
	}
	for i, st := range u.states {
		st.scanning, st.items, st.err = false, results[i].items, results[i].err
	}
	if *shotDark {
		u.applyAppearance(appearanceDark) // the same as the "Dark" tile in settings
	}
	u.win.Resize(fyne.NewSize(1180, 780))
	u.render()

	save := func(name string) {
		variant := "light"
		if *shotDark {
			variant = "dark"
		}
		f, err := os.Create(filepath.Join(*shotDir, *shotLang+"-"+variant+"-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, u.win.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}

	// One screenshot per provider.
	for i, st := range u.states {
		u.selectProvider(i)
		save(st.provider.ID())
	}

	// Scanning: the first two sections pretend to be still scanning, to show
	// the sidebar spinners, the ring (nothing found yet) and live progress;
	// then their results come back.
	home := kit.Home()
	for _, st := range u.states[:2] {
		st.scanning = true
		st.progress = &scanProgress{}
	}
	u.states[0].progress.Looking(filepath.Join(home, "Projects"))
	for _, p := range []string{"Projects/Lingary/node_modules", "Projects/Lingary/ios/Pods", "Projects/courses/.expo", "Projects/remote-desktop/build"} {
		u.states[1].progress.Looking(filepath.Join(home, p))
	}
	u.states[1].progress.Measured("", 812_400_000)
	u.refresh()
	for i, name := range []string{"scanning-orbit", "scanning-live"} {
		u.selectProvider(i)
		u.states[i].view.update()
		u.relayout()
		save(name)
	}
	for _, st := range u.states[:2] {
		st.scanning = false
		st.stopAnimations()
	}
	u.refresh()

	// An expanded category: explanations and details.
	u.selectProvider(0)
	if cats := u.states[0].provider.Categories(); len(cats) > 0 {
		u.expanded[cats[0].ID] = true
		u.refreshDetails()
		save("expanded")
		u.expanded[cats[0].ID] = false
	}

	// Search in a section and global search.
	u.setSectionQuery("lingary")
	u.relayout()
	save("section-search")
	u.searchEntry.SetText("lingary")
	u.relayout()
	save("global-search")
	u.searchEntry.SetText("")

	// Settings with the appearance choice; then we close the settings window.
	u.showSettings()
	save("settings")
	u.win.Canvas().Overlays().Remove(u.win.Canvas().Overlays().Top())

	// The confirmation window: we select up to three items of the first provider.
	u.selectProvider(0)
	for _, it := range u.states[0].items {
		if ok, _ := it.CanSelect(); ok && u.sel.Len() < 3 {
			_ = u.sel.Add(it)
		}
	}
	u.refresh()
	u.reviewAndClean()
	save("confirm")
	u.win.Canvas().Overlays().Remove(u.win.Canvas().Overlays().Top())

	// The review window from the "Clean All Safe" button in the sidebar.
	u.cleanAll(u.states...)
	save("clean-all")
	u.win.Canvas().Overlays().Remove(u.win.Canvas().Overlays().Top())

	// A locked item (on the Never clean list): the first item of the last
	// section that has any, locked with its row's lock and unlocked again.
	for i := len(u.states) - 1; i >= 0; i-- {
		if items := u.states[i].items; len(items) > 0 {
			u.selectProvider(i)
			u.addExclusions(items[0])
			save("locked")
			u.allowCleaning(u.states[i].items[0])
			break
		}
	}

	// A new version: the card in the sidebar and the update window, with a
	// fake GitHub that has three new commits.
	u.opts.Updater = &fakeUpdater{news: update.News{Count: 3, Titles: []string{
		"Scan view in the middle of the free space",
		"Scan view: ring of dots until something is found, then live progress",
		"Lock button on each row for Never clean",
	}}}
	u.news = update.News{Count: 3, Titles: u.opts.Updater.(*fakeUpdater).news.Titles}
	u.fillUpdate()
	save("update-card")
	u.showUpdate()
	save("update")
	u.win.Canvas().Overlays().Remove(u.win.Canvas().Overlays().Top())
	u.news = update.News{}
	u.fillUpdate()

	// Settings scrolled to the end: the scrollbar and shadows run edge to edge.
	u.showSettings()
	save("settings")
	u.win.Canvas().Overlays().Remove(u.win.Canvas().Overlays().Top())

	// The setup window before the first scan, with project folders found on this Mac.
	u.setupSheet(true)
	save("intro-finding") // the search runs in the background…
	time.Sleep(8 * time.Second)
	save("intro") // …and here its result is in the list
}
