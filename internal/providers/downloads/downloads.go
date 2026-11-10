// Package downloads is the provider for the Downloads folder: installers (.dmg, .pkg),
// app builds (.aab, .apk, .ipa, .xcarchive, .app) and archives
// (.zip, .xip, .tar.gz…).
//
// These are the user's files, so the rules are stricter than for caches:
//   - nothing is deleted for good: files are moved to the Trash, and any
//     mistake can be fixed by putting the file back;
//   - a mounted disk image (a .dmg open in Finder) cannot be selected;
//   - to read Downloads, macOS asks for permission once. If it is
//     not given, the section calmly says where to allow it.
package downloads

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/shell"
)

//go:embed translations
var translations embed.FS

const (
	catInstallers = "downloads.installers"
	catBuilds     = "downloads.builds"
	catArchives   = "downloads.archives"

	reasonNoAccess  = "downloads.no_access"  // macOS did not give access to Downloads
	reasonTurnedOff = "downloads.turned_off" // switched off in Settings › Permissions
	reasonMounted   = "downloads.mounted"    // the disk image is mounted now

	minSize = core.Bytes(1_000_000) // we do not show small stuff
)

// kinds are which name endings go into which category.
// For archives, compound endings (.tar.gz) must be checked before simple ones.
var kinds = []struct {
	category string
	exts     []string
}{
	{catInstallers, []string{".dmg", ".pkg", ".mpkg", ".iso"}},
	{catBuilds, []string{".aab", ".apk", ".ipa", ".xcarchive", ".app", ".appimage", ".exe", ".msi"}},
	{catArchives, []string{".tar.gz", ".tar.xz", ".tar.bz2", ".zip", ".xip", ".tgz", ".tar", ".gz", ".7z", ".rar"}},
}

// Provider looks for files in Downloads.
type Provider struct {
	dir     string       // the Downloads folder
	run     shell.Runner // runs hdiutil; a fake in tests
	enabled func() bool  // the switch in Settings › Permissions; nil means on
}

// New is the provider for the current user's Downloads. enabled is asked
// before every scan: when it says no, Downloads isn't even opened, so macOS
// doesn't ask for access either.
func New(enabled func() bool) *Provider {
	return &Provider{dir: filepath.Join(kit.Home(), "Downloads"), run: shell.Run, enabled: enabled}
}

func (p *Provider) ID() string { return "downloads" }

func (p *Provider) Translations() embed.FS { return translations }

func (p *Provider) Categories() []core.Category {
	return []core.Category{
		{ID: catInstallers, Risk: core.RiskSafe},  // the app is already installed or can be downloaded again
		{ID: catBuilds, Risk: core.RiskCaution},   // the build may not be uploaded to the store yet
		{ID: catArchives, Risk: core.RiskCaution}, // there may be something unique inside
	}
}

func (p *Provider) Scan(ctx context.Context) ([]core.Item, error) {
	if p.enabled != nil && !p.enabled() {
		return nil, core.Unavailable(reasonTurnedOff)
	}

	entries, err := os.ReadDir(p.dir)
	if errors.Is(err, fs.ErrPermission) {
		// macOS did not give access: this is not a failure but a question of permission.
		return nil, core.Unavailable(reasonNoAccess)
	}
	if err != nil {
		return nil, err
	}

	mounted := p.mountedImages(ctx)

	var items []core.Item
	var paths []string
	for _, e := range entries {
		cat := categoryOf(e.Name())
		if cat == "" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Only .app and .xcarchive builds can be folders; a folder "my.zip" is not an archive.
		if e.IsDir() && cat != catBuilds {
			continue
		}

		path := filepath.Join(p.dir, e.Name())
		it := core.Item{ID: path, CategoryID: cat, Name: e.Name()}
		if info, err := e.Info(); err == nil {
			it.Detail = info.ModTime().Format("2006-01-02") // when it was downloaded or modified
		}
		if mounted[path] {
			it.Status = core.StatusInUse
			it.Busy = &core.Reason{Key: reasonMounted}
		}
		items = append(items, it)
		paths = append(paths, path)
	}

	sizes := diskutil.SizeAll(ctx, paths, kit.Workers())
	for i := range items {
		items[i].Size = sizes[i]
	}
	items = slices.DeleteFunc(items, func(it core.Item) bool { return it.Size < minSize })
	slices.SortFunc(items, func(a, b core.Item) int { return cmp.Compare(b.Size, a.Size) })

	return items, nil
}

// Plan: to the Trash, not for good.
func (p *Provider) Plan(items []core.Item) ([]core.Action, error) {
	actions := make([]core.Action, 0, len(items))
	for _, it := range items {
		actions = append(actions, core.Action{RemovePath: it.ID, Trash: true})
	}
	return actions, nil
}

// categoryOf is the category by name ending ("" means not our file).
func categoryOf(name string) string {
	lower := strings.ToLower(name)
	for _, k := range kinds {
		for _, ext := range k.exts {
			if strings.HasSuffix(lower, ext) {
				return k.category
			}
		}
	}
	return ""
}

// mountedImages are the disk images that are mounted right now. hdiutil info
// prints a line "image-path : /Users/…/file.dmg" for each one.
func (p *Provider) mountedImages(ctx context.Context) map[string]bool {
	out, err := p.run(ctx, "/usr/bin/hdiutil", "info")
	if err != nil {
		return nil
	}

	mounted := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(key) == "image-path" {
			mounted[strings.TrimSpace(value)] = true
		}
	}
	return mounted
}
