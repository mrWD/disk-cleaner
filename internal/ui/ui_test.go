package ui

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

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

// keyRe finds strings in the code that look like translation keys: "button.rescan".
// Import paths ("fyne.io/fyne/v2") don't match: they contain / and -.
var keyRe = regexp.MustCompile(`"([a-z][a-z_]*(?:\.[a-z_]+)+)"`)

// pluralRe finds keys that have a form for the singular: plural("key", n).
var pluralRe = regexp.MustCompile(`plural\("([a-z_.]+)"`)

// notTextKey matches file names, which keyRe also finds.
var notTextKey = regexp.MustCompile(`\.(svg|png|json|otf|ttf|io|com)$`) // files and domains (mrwd.github.io)

// TestEveryKeyIsTranslated checks that every key found in the UI code
// exists in all languages. It catches typos like "core.reasong.in_use".
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
		// For plural("key", n) the variant "key.one" is needed as well.
		for _, m := range pluralRe.FindAllStringSubmatch(string(src), -1) {
			keys = append(keys, m[1]+".one")
		}
		for _, m := range keyRe.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			// Not translation keys: our own theme colour and size names ("dc.card")
			// and file names ("icon.svg").
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

// fakeProvider is a provider for testing the window. Its background scan
// never finishes: the test supplies the result itself (loaded),
// so that background goroutines don't disturb the checks.
type fakeProvider struct {
	id    string
	items []core.Item
}

func (f fakeProvider) ID() string { return f.id }
func (f fakeProvider) Categories() []core.Category {
	return []core.Category{{ID: f.id + ".cache", Risk: core.RiskSafe}, {ID: f.id + ".empty", Risk: core.RiskCaution}}
}
func (fakeProvider) Scan(ctx context.Context) ([]core.Item, error) {
	<-ctx.Done() // wait forever: the context is a background one and is never cancelled
	return nil, ctx.Err()
}

// Plan: one removal per item; items whose ID starts with "admin" need the password.
func (fakeProvider) Plan(items []core.Item) ([]core.Action, error) {
	var out []core.Action
	for _, it := range items {
		out = append(out, core.Action{RemovePath: "/tmp/" + it.ID, NeedsAdmin: strings.HasPrefix(it.ID, "admin")})
	}
	return out, nil
}
func (fakeProvider) Translations() embed.FS { return embed.FS{} }

// texts collects all the texts visible in the widget tree.
func texts(obj fyne.CanvasObject) []string {
	var out []string
	switch o := obj.(type) { // a switch on the type of the value
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

// Objects for the scenarios: two sections, "xcode" and "docker".
var (
	derived  = core.Item{ID: "d1", CategoryID: "xcode.cache", Name: "Lingary", Detail: "~/Library/Developer/Xcode/DerivedData/Lingary-abc", Size: 2_000_000}
	archive  = core.Item{ID: "d2", CategoryID: "xcode.cache", Name: "SpesaPlan 1.0.0", Detail: "~/Library/Developer/Xcode/Archives/SpesaPlan", Size: 1_000_000}
	redis    = core.Item{ID: "c1", CategoryID: "docker.cache", Name: "redis · redis:8", Status: core.StatusRunning, Size: 1_000}
	postgres = core.Item{ID: "c2", CategoryID: "docker.cache", Name: "postgres:17", Detail: "Lingary database", Size: 3_000_000}
)

// loaded is a window with two sections that already have scan results.
func loaded(t *testing.T) *App {
	t.Helper()

	a := test.NewTempApp(t) // an app without a real screen, for tests
	store := settings.New(a.Preferences(), t.TempDir())
	store.SetLanguage("en")
	store.SetPermissionsAsked(true) // not the first launch: no setup window
	store.SetRoots(nil)

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

// shown is all the texts of the right-hand part as one string.
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
		// Given: the docker section is open
		u := loaded(t)
		u.selectProvider(1)

		// Then: the name, path, short label and size are visible; there are no long explanations
		mustShow(t, u, "redis · redis:8", "Running", "1.0 kB", "postgres:17", "Lingary database", "2 items")
		mustNotShow(t, u, "What it is.", "Running right now.")
	})

	t.Run("expanding a category shows the explanation and the full reason", func(t *testing.T) {
		// Given: the docker section is open
		u := loaded(t)
		u.selectProvider(1)

		// When: we expand the category
		u.expanded["docker.cache"] = true
		u.refreshDetails()

		// Then: the explanations and the full reason for the block are visible
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
		// Given: the xcode section is open
		u := loaded(t)

		// When: we search by path
		u.setSectionQuery("archives")

		// Then: only the archive is left
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

		// When: we search for a word that is in both Xcode and Docker
		u.setGlobalQuery("lingary")

		// Then: results from both sections, with section headers
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
		// Given: global search shows postgres and Lingary, and redis is running
		u := loaded(t)
		u.setGlobalQuery("lingary")

		// When: "Select All"
		u.toggleVisibleSelection()

		// Then: both found items are selected, but not the archive (it isn't visible) and not redis
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
		// Given: macOS in the light look (the test app is light)
		u := loaded(t)

		// When: we choose the dark appearance
		u.applyAppearance(appearanceDark)

		// Then: the theme returns dark colours even when asked about the light look
		if got := u.theme.Color(theme.ColorNameBackground, theme.VariantLight); got != darkPalette.background {
			t.Errorf("background = %v, want dark %v", got, darkPalette.background)
		}
	})

	t.Run("settings offer three options and remember the choice", func(t *testing.T) {
		// Given: "Auto" is saved
		u := loaded(t)
		picker := u.newAppearancePicker(appearanceSystem)

		// Then: three tiles: Light, Dark, Auto
		if got := strings.Join(texts(picker.box), ","); got != "Light,Dark,Auto" {
			t.Errorf("tiles = %q, want Light,Dark,Auto", got)
		}

		// When: we choose "Dark" and save
		picker.pick(appearanceDark)
		u.setAppearance(picker.selected)

		// Then: the dark look is on and remembered
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
		// Given: Lingary is checked in the xcode section
		u := loaded(t)
		_ = u.sel.Add(derived)
		u.updateBottom()

		// When: we open the docker section
		u.selectProvider(1)

		// Then: nothing is selected, and the bottom strip shows it
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
		// Given: Lingary is checked
		u := loaded(t)
		_ = u.sel.Add(derived)

		// When: we expanded the category, changed the appearance (the window was rebuilt)
		// and opened the same section again
		u.expanded["xcode.cache"] = true
		u.refreshDetails()
		u.setAppearance(appearanceDark)
		u.render()
		u.selectProvider(0)

		// Then: the mark is still there
		if !u.sel.Contains(derived) {
			t.Errorf("selection = %v, want Lingary kept", u.sel.Items())
		}
	})
}

func TestCleanAll(t *testing.T) {
	// withCaution is the same set plus an item in the "Caution" category.
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

		u.cleanAll(u.states...) // the review window opens; "Cancel" changes nothing

		if u.sel.Len() != 0 {
			t.Errorf("selection = %v, want untouched", u.sel.Items())
		}
	})

	t.Run("buttons are off while scanning and when nothing is safe", func(t *testing.T) {
		u := loaded(t)
		if u.cleanEvery.Disabled() || u.cleanAllBtn.Disabled() {
			t.Fatal("both Clean All buttons should be on when safe items are found")
		}

		// When: the docker section is scanned again
		u.states[1].scanning = true
		u.refresh()

		// Then: the shared button waits for the scan to end, and the xcode section's button doesn't
		if !u.cleanEvery.Disabled() {
			t.Error("global Clean All should wait until scanning finishes")
		}
		if u.cleanAllBtn.Disabled() {
			t.Error("Clean All in xcode should stay on: xcode is not scanning")
		}

		// When: only "Caution" is left in xcode
		u.states[0].items = []core.Item{emulator}
		u.refresh()

		// Then: the section's button is disabled
		if !u.cleanAllBtn.Disabled() {
			t.Error("Clean All in xcode should be off: nothing safe there")
		}
	})
}

func TestNeverClean(t *testing.T) {
	t.Run("Never Clean protects the selected items and remembers them", func(t *testing.T) {
		// Given: Lingary is selected
		u := loaded(t)
		_ = u.sel.Add(derived)

		// When: Never Clean
		u.neverClean()

		// Then: it can't be selected, isn't in Clean All Safe, and is on the list
		got := byID(u, derived.ID)
		if ok, why := got.CanSelect(); ok || why.Key != reasonExcluded {
			t.Errorf("Lingary: selectable = %v, reason = %+v; want protected by Never clean", ok, why)
		}
		if u.sel.Len() != 0 {
			t.Errorf("selection = %v, want empty", u.sel.Items())
		}
		if u.safeSelection(u.states...).Contains(got) {
			t.Error("Clean All Safe must skip Never clean items")
		}
		if list := u.opts.Settings.Excluded(); len(list) != 1 || list[0].ID != derived.ID {
			t.Errorf("saved list = %v", list)
		}
	})

	t.Run("Allow cleaning takes it off the list", func(t *testing.T) {
		u := loaded(t)
		_ = u.sel.Add(derived)
		u.neverClean()

		u.allowCleaning(byID(u, derived.ID))

		if ok, _ := byID(u, derived.ID).CanSelect(); !ok {
			t.Error("Lingary should be selectable again")
		}
		if len(u.opts.Settings.Excluded()) != 0 {
			t.Errorf("list = %v, want empty", u.opts.Settings.Excluded())
		}
	})

	t.Run("a folder covers what's inside it and what contains it, nothing else", func(t *testing.T) {
		// Given: the folder ~/Projects/app is on the list
		u := loaded(t)
		u.opts.Settings.SetExcluded([]settings.Exclusion{{ID: "/Users/me/Projects/app", Name: "app", Detail: "~/Projects/app"}})
		u.states[0].items = []core.Item{
			{ID: "/Users/me/Projects/app/node_modules", CategoryID: "xcode.cache", Name: "inside", Size: 1},
			{ID: "/Users/me/Projects", CategoryID: "xcode.cache", Name: "contains", Size: 1},
			{ID: "/Users/me/Projects/application/node_modules", CategoryID: "xcode.cache", Name: "neighbour", Size: 1},
		}

		// When: the list is applied
		u.applyExclusions()

		// Then
		for _, name := range []string{"inside", "contains"} {
			if ok, why := byName(u, name).CanSelect(); ok || why.Key != reasonExcludedFolder || why.Args["Folder"] != "~/Projects/app" {
				t.Errorf("%s: selectable = %v, reason = %+v; want protected by the folder", name, ok, why)
			}
		}
		if ok, _ := byName(u, "neighbour").CanSelect(); !ok {
			t.Error("~/Projects/application is not inside ~/Projects/app and must stay selectable")
		}
	})

	t.Run("the lock works for a running item that can't be ticked", func(t *testing.T) {
		// Given: redis is running, so its checkbox is disabled
		u := loaded(t)

		// When: its lock is clicked
		u.addExclusions(byID(u, redis.ID))

		// Then: it's on the list and shows that reason
		if got := byID(u, redis.ID); !isExclusion(got.Protected) {
			t.Errorf("redis protection = %+v, want Never clean", got.Protected)
		}

		// When: the lock is clicked again
		u.allowCleaning(byID(u, redis.ID))

		// Then: it's back to being just Running
		if _, why := byID(u, redis.ID).CanSelect(); why == nil || why.Key != core.ReasonRunning {
			t.Errorf("redis reason = %+v, want running", why)
		}
	})

	t.Run("a provider's own protection is kept", func(t *testing.T) {
		u := loaded(t)
		own := core.Item{ID: "p1", CategoryID: "xcode.cache", Name: "provider protected", Protected: &core.Reason{Key: "core.reason.in_use"}}
		u.states[0].items = append(u.states[0].items, own)
		u.opts.Settings.SetExcluded([]settings.Exclusion{{ID: "p1"}})

		u.applyExclusions()
		u.opts.Settings.SetExcluded(nil)
		u.applyExclusions()

		if got := byID(u, "p1"); got.Protected == nil || got.Protected.Key != "core.reason.in_use" {
			t.Errorf("protection = %+v, want the provider's reason kept", got.Protected)
		}
	})
}

func TestPermissions(t *testing.T) {
	t.Run("first launch asks before scanning", func(t *testing.T) {
		// Given: a fresh install
		a := test.NewTempApp(t)
		store := settings.New(a.Preferences(), t.TempDir())
		store.SetLanguage("en")

		// When: the window opens
		u, err := New(a, Options{Providers: []core.Provider{fakeProvider{id: "xcode"}}, Settings: store})
		if err != nil {
			t.Fatal(err)
		}

		// Then: the permissions window is shown and nothing is scanned yet
		if u.win.Canvas().Overlays().Top() == nil {
			t.Error("the permissions window should be shown on first launch")
		}
		if u.states[0].scanning {
			t.Error("scanning should wait until the user continues")
		}

		// When: Continue with one project folder and Downloads off
		u.finishSetup([]string{"/Users/me/work"}, false, true)

		// Then: the choices are saved and the scan starts
		if roots := store.Roots(); len(roots) != 1 || roots[0] != "/Users/me/work" || !store.RootsConfirmed() {
			t.Errorf("roots = %v, confirmed = %v", roots, store.RootsConfirmed())
		}
		if store.UseDownloads() || !store.AllowAdmin() || !store.PermissionsAsked() {
			t.Errorf("saved: downloads = %v, admin = %v, asked = %v", store.UseDownloads(), store.AllowAdmin(), store.PermissionsAsked())
		}
		if !u.states[0].scanning {
			t.Error("the scan should start after Continue")
		}
	})

	t.Run("with the administrator password off, such items are left out", func(t *testing.T) {
		// Given: one item needs the password, and that's turned off
		u := loaded(t)
		needsPassword := core.Item{ID: "admin-dyld", CategoryID: "xcode.cache", Name: "dyld cache", Size: 10}
		u.states[0].items = append(u.states[0].items, needsPassword)
		u.opts.Settings.SetAllowAdmin(false)
		sel := u.safeSelection(u.states[0])

		// When
		left, skipped := u.withoutAdmin(sel)

		// Then
		if skipped != 1 || left.Contains(needsPassword) || !left.Contains(derived) {
			t.Errorf("skipped = %d, left = %v", skipped, left.Items())
		}
	})
}

// byID and byName find an item in the window's current results.
func byID(u *App, id string) core.Item {
	for _, st := range u.states {
		for _, it := range st.items {
			if it.ID == id {
				return it
			}
		}
	}
	return core.Item{}
}

func byName(u *App, name string) core.Item {
	for _, st := range u.states {
		for _, it := range st.items {
			if it.Name == name {
				return it
			}
		}
	}
	return core.Item{}
}

func TestProjectFolders(t *testing.T) {
	t.Run("the setup window starts from the folders found on this Mac", func(t *testing.T) {
		// Given: the search finds two folders
		u := loaded(t)
		u.opts.FindProjectFolders = func() []string { return []string{"/Users/me/my-projects", "/Users/me/work"} }

		// When: the list starts with a search
		var roots []string
		u.projectFolderList(&roots, true)

		// Then: once the background search is done, both are in the list
		deadline := time.Now().Add(2 * time.Second)
		for len(roots) < 2 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !slices.Equal(roots, []string{"/Users/me/my-projects", "/Users/me/work"}) {
			t.Errorf("roots = %v", roots)
		}
	})

	t.Run("confirming an empty list means no folders, not the old guess", func(t *testing.T) {
		u := loaded(t)

		u.finishSetup(nil, true, true)

		if roots := u.opts.Settings.Roots(); len(roots) != 0 {
			t.Errorf("roots = %v, want none", roots)
		}
	})
}

func TestScanAnimation(t *testing.T) {
	t.Run("the animation survives rebuilds, so it doesn't restart", func(t *testing.T) {
		// Given: docker is scanning and open
		u := loaded(t)
		docker := u.states[1]
		docker.scanning = true
		u.selectProvider(1)
		spinner, view := docker.spinner, docker.view
		if spinner == nil || view == nil {
			t.Fatal("a scanning section should show a spinner in the sidebar and its scan view")
		}

		// When: another section finishes and the window is rebuilt
		u.finishScan(u.states[0], u.states[0].gen, []core.Item{derived}, nil)

		// Then: still the very same objects
		if docker.spinner != spinner || docker.view != view {
			t.Error("spinner and scan view were made anew, so their animation would start over")
		}
	})

	t.Run("the ring shows until something is measured, then live progress", func(t *testing.T) {
		// Given: docker is scanning, nothing measured yet
		u := loaded(t)
		docker := u.states[1]
		docker.scanning = true
		docker.progress = &scanProgress{}
		u.selectProvider(1)
		docker.progress.Looking("/Users/me/.docker/build")
		docker.view.update()

		// Then: the ring, with the folder being looked at under it
		if !docker.view.orbit.Visible() || docker.view.live.Visible() || docker.view.nowAt.Text == "" {
			t.Errorf("before anything is measured: orbit %v, live %v, now at %q",
				docker.view.orbit.Visible(), docker.view.live.Visible(), docker.view.nowAt.Text)
		}

		// When: a folder of 5 MB is measured, then the next one is looked at
		docker.progress.Measured("/Users/me/.docker/build", 5_000_000)
		docker.progress.Looking("/Users/me/.docker/volumes")
		docker.view.update()

		// Then: the live view with the total, newest folder first
		if docker.view.orbit.Visible() || !docker.view.live.Visible() || docker.view.total.Text != "5.0 MB" {
			t.Errorf("after 5 MB: orbit %v, live %v, total %q", docker.view.orbit.Visible(), docker.view.live.Visible(), docker.view.total.Text)
		}
		if got := docker.view.paths[0].Text; got != "/Users/me/.docker/volumes" {
			t.Errorf("newest path = %q", got)
		}
	})

	t.Run("they go away when the scan ends", func(t *testing.T) {
		u := loaded(t)
		docker := u.states[1]
		docker.scanning = true
		u.selectProvider(1)

		u.finishScan(docker, docker.gen, []core.Item{postgres}, nil)

		if docker.spinner != nil || docker.view != nil || len(docker.anims) != 0 {
			t.Error("animations should stop and be forgotten when the scan ends")
		}
		mustShow(t, u, "postgres:17")
	})
}
