// Package settings — настройки приложения, которые переживают перезапуск:
// язык, оформление, где искать проекты, режим «только показать».
//
// Хранятся через Preferences из Fyne (на macOS это обычный файл настроек
// приложения). Поэтому у приложения должен быть ID: app.NewWithID(...).
package settings

import (
	"path/filepath"

	"fyne.io/fyne/v2"
)

// Ключи в хранилище настроек.
const (
	keyLanguage = "language"
	keyRoots    = "worktree_roots"
	keyDryRun   = "dry_run"

	keyAppearance = "appearance"
)

// Store читает и пишет настройки.
type Store struct {
	prefs fyne.Preferences
	home  string
}

// New — настройки поверх хранилища Fyne. home нужна для папки по умолчанию.
func New(prefs fyne.Preferences, home string) *Store {
	return &Store{prefs: prefs, home: home}
}

// Language — выбранный язык ("" — как в macOS).
func (s *Store) Language() string { return s.prefs.String(keyLanguage) }

// SetLanguage сохраняет выбранный язык.
func (s *Store) SetLanguage(lang string) { s.prefs.SetString(keyLanguage, lang) }

// Appearance — оформление: "" — как в macOS, "light" — светлое, "dark" — тёмное.
func (s *Store) Appearance() string { return s.prefs.String(keyAppearance) }

// SetAppearance сохраняет оформление.
func (s *Store) SetAppearance(mode string) { s.prefs.SetString(keyAppearance, mode) }

// Roots — папки, в которых искать git worktree. По умолчанию ~/Projects.
func (s *Store) Roots() []string {
	roots := s.prefs.StringList(keyRoots)
	if len(roots) == 0 {
		return []string{filepath.Join(s.home, "Projects")}
	}
	return roots
}

// SetRoots сохраняет папки для поиска worktree.
func (s *Store) SetRoots(roots []string) { s.prefs.SetStringList(keyRoots, roots) }

// DryRun — включён ли режим «только показать, ничего не удалять».
func (s *Store) DryRun() bool { return s.prefs.Bool(keyDryRun) }

// SetDryRun включает или выключает этот режим.
func (s *Store) SetDryRun(on bool) { s.prefs.SetBool(keyDryRun, on) }
