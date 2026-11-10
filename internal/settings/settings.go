// Package settings holds the app settings that survive a restart:
// language, appearance, where to look for projects, the "Dry run" mode.
//
// They are stored through Fyne's Preferences (on macOS this is an ordinary
// app preferences file). That is why the app must have an ID: app.NewWithID(...).
package settings

import (
	"encoding/json"
	"path/filepath"

	"fyne.io/fyne/v2"
)

// Keys in the settings storage.
const (
	keyLanguage = "language"
	keyRoots    = "worktree_roots"
	keyDryRun   = "dry_run"

	keyAppearance = "appearance"

	keyExcluded         = "never_clean"
	keyUseDownloads     = "use_downloads"
	keyAllowAdmin       = "allow_admin"
	keyPermissionsAsked = "permissions_asked"
	keyRootsConfirmed   = "worktree_roots_confirmed"
)

// Store reads and writes the settings.
type Store struct {
	prefs fyne.Preferences
	home  string
}

// New creates settings on top of Fyne's storage. home is needed for the default folder.
func New(prefs fyne.Preferences, home string) *Store {
	return &Store{prefs: prefs, home: home}
}

// Language is the chosen language ("" means same as macOS).
func (s *Store) Language() string { return s.prefs.String(keyLanguage) }

// SetLanguage saves the chosen language.
func (s *Store) SetLanguage(lang string) { s.prefs.SetString(keyLanguage, lang) }

// Appearance is the look: "" means same as macOS, "light" is light, "dark" is dark.
func (s *Store) Appearance() string { return s.prefs.String(keyAppearance) }

// SetAppearance saves the appearance.
func (s *Store) SetAppearance(mode string) { s.prefs.SetString(keyAppearance, mode) }

// Roots are the folders to search for git worktrees. The default is ~/Projects.
func (s *Store) Roots() []string {
	roots := s.prefs.StringList(keyRoots)
	if len(roots) == 0 && !s.RootsConfirmed() {
		return []string{filepath.Join(s.home, "Projects")} // nothing chosen yet
	}
	return roots // what the user confirmed, even if it's an empty list
}

// RootsConfirmed — whether the user has confirmed the project folders (in the
// first-launch window or with Save in Settings). Until then Roots is a guess.
func (s *Store) RootsConfirmed() bool { return s.prefs.Bool(keyRootsConfirmed) }

// SetRoots saves the folders to search for worktrees.
func (s *Store) SetRoots(roots []string) {
	s.prefs.SetStringList(keyRoots, roots)
	s.prefs.SetBool(keyRootsConfirmed, true)
}

// DryRun tells whether the "only show, delete nothing" mode (Dry run) is on.
func (s *Store) DryRun() bool { return s.prefs.Bool(keyDryRun) }

// SetDryRun turns this mode on or off.
func (s *Store) SetDryRun(on bool) { s.prefs.SetBool(keyDryRun, on) }

// Exclusion is something the user asked never to clean: one item, or a
// folder with everything inside it.
type Exclusion struct {
	ID     string `json:"id"`     // the item's ID; for files and folders, the full path
	Name   string `json:"name"`   // first line in Settings
	Detail string `json:"detail"` // second line in Settings, usually the path with ~
}

// Excluded is the "Never clean" list. Preferences keep only simple values,
// so the list is stored as one JSON string.
func (s *Store) Excluded() []Exclusion {
	var list []Exclusion
	_ = json.Unmarshal([]byte(s.prefs.String(keyExcluded)), &list) // nothing saved yet → empty list
	return list
}

// SetExcluded saves the "Never clean" list.
func (s *Store) SetExcluded(list []Exclusion) {
	data, _ := json.Marshal(list) // plain strings can always be encoded
	s.prefs.SetString(keyExcluded, string(data))
}

// UseDownloads — whether Disk Cleaner looks in Downloads (on by default).
func (s *Store) UseDownloads() bool { return s.prefs.BoolWithFallback(keyUseDownloads, true) }

// SetUseDownloads turns looking in Downloads on or off.
func (s *Store) SetUseDownloads(on bool) { s.prefs.SetBool(keyUseDownloads, on) }

// AllowAdmin — whether actions that need the administrator password are
// allowed (on by default). When off, such items are skipped.
func (s *Store) AllowAdmin() bool { return s.prefs.BoolWithFallback(keyAllowAdmin, true) }

// SetAllowAdmin allows or forbids those actions.
func (s *Store) SetAllowAdmin(on bool) { s.prefs.SetBool(keyAllowAdmin, on) }

// PermissionsAsked — whether the first-launch window about permissions was shown.
func (s *Store) PermissionsAsked() bool { return s.prefs.Bool(keyPermissionsAsked) }

// SetPermissionsAsked remembers that it was shown.
func (s *Store) SetPermissionsAsked(done bool) { s.prefs.SetBool(keyPermissionsAsked, done) }
