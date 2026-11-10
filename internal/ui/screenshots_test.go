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

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/executor"
	"disk-cleaner/internal/providers/android"
	"disk-cleaner/internal/providers/appcaches"
	"disk-cleaner/internal/providers/devtools"
	"disk-cleaner/internal/providers/docker"
	"disk-cleaner/internal/providers/downloads"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/providers/models"
	"disk-cleaner/internal/providers/packagecaches"
	"disk-cleaner/internal/providers/projects"
	"disk-cleaner/internal/providers/simulators"
	"disk-cleaner/internal/providers/worktrees"
	"disk-cleaner/internal/providers/xcode"
	"disk-cleaner/internal/settings"
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
	store.SetPermissionsAsked(true) // the first-launch window is captured separately at the end
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

	u, err := New(a, Options{Providers: wrapped, Executor: ex, Settings: store})
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

	// The first-launch window about permissions.
	u.showIntro()
	save("intro")
}
