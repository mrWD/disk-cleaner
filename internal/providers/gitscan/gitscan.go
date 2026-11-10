// Package gitscan finds git repositories and "artifact" folders
// (node_modules, Pods, build...) in them. Two providers use it:
// worktrees (extra working copies) and projects (main
// repositories). Providers don't import each other, but they may import this helper.
//
// The main rule for both: ONLY what git ignores may be deleted
// (git check-ignore). Tracked files are never touched, even if
// the folder is called build.
package gitscan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"disk-cleaner/internal/shell"
)

// Git is the path to git. A Mac with Xcode always has it.
const Git = "/usr/bin/git"

// MinSize: artifacts smaller than this are not shown. Dozens of
// tiny folders only clutter the list and free almost no space.
const MinSize = 1_000_000

// artifactNames are folders that are usually created by an install or a build.
var artifactNames = map[string]bool{
	"node_modules": true, "Pods": true, "build": true, ".gradle": true,
	".cxx": true, ".expo": true, ".next": true, ".turbo": true,
	"dist": true, "DerivedData": true, ".venv": true, "venv": true,
	"target": true, "release": true, "out": true, // release/, out/ are the output of electron-builder and others
}

// buildExts are extensions of finished app builds: Xcode archives,
// iOS and Android packages, macOS and Windows installers.
var buildExts = []string{
	".xcarchive", ".ipa", ".app", ".apk", ".aab", ".dmg", ".pkg",
	".exe", ".msi", ".appimage", ".deb",
}

// isBuild reports whether the name looks like a finished build.
func isBuild(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range buildExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// skipWhileSearching lists where we don't go when looking for repositories:
// there are none there, but there are millions of files.
var skipWhileSearching = map[string]bool{
	"node_modules": true, "Pods": true, "build": true, ".gradle": true,
	"DerivedData": true, ".venv": true, "venv": true, "target": true,
	"dist": true, ".next": true, "vendor": true, ".git": true, "Library": true,
}

const (
	maxSearchDepth   = 6 // how deep from the root to look for repositories
	maxArtifactDepth = 4 // how deep inside a repository to look for artifacts
)

// Kind is the kind of repository.
type Kind int

const (
	Main     Kind = iota // main: .git is a folder
	Worktree             // extra worktree: .git is a file "gitdir: .../worktrees/..."
)

// Repo is a repository that was found.
type Repo struct {
	Path string
	Kind Kind
}

// KindOf reports whether dir is a repository, and which kind. ok = false means it is not a repository
// (or it is a submodule: its .git is also a file, but the path leads into .git/modules).
func KindOf(dir string) (Kind, bool) {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return 0, false
	}
	if info.IsDir() {
		return Main, true
	}

	data, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(string(data))
	if strings.HasPrefix(line, "gitdir:") && strings.Contains(line, "/worktrees/") {
		return Worktree, true
	}
	return 0, false
}

// FindRepos looks for repositories inside root. It keeps going into a main repository
// (.claude/worktrees and nested repositories can be inside),
// but not into a worktree.
func FindRepos(root string) []Repo {
	var found []Repo

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if rel != "." && (skipWhileSearching[d.Name()] || strings.Count(rel, "/") >= maxSearchDepth) {
			return fs.SkipDir
		}

		kind, ok := KindOf(path)
		if !ok {
			return nil
		}
		found = append(found, Repo{Path: path, Kind: kind})
		if kind == Worktree {
			return fs.SkipDir // we don't look for other worktrees inside a worktree
		}
		return nil
	})

	return found
}

// FindArtifacts returns the paths (relative to repo) of folders named in artifactNames.
// It does not go inside a found one: node_modules inside node_modules is not needed.
// It does not go into nested repositories and worktrees either: they have their own owner.
func FindArtifacts(repo string) []string {
	var found []string

	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == repo {
			return nil
		}

		rel, _ := filepath.Rel(repo, path)
		switch {
		case d.Name() == ".git":
			return fs.SkipDir
		case artifactNames[d.Name()]:
			found = append(found, rel)
			return fs.SkipDir
		case strings.Count(rel, "/") >= maxArtifactDepth-1:
			return fs.SkipDir
		}
		if _, nested := KindOf(path); nested {
			return fs.SkipDir // another repository, not ours
		}
		return nil
	})

	return found
}

// FindBuilds returns finished builds (.xcarchive, .apk, .dmg, .app...) that
// lie in the repository outside artifact folders: for example an archive saved
// right in the project root. Paths are relative to repo. It does not go inside a found
// build (.app and .xcarchive are folders).
func FindBuilds(repo string) []string {
	var found []string

	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == repo {
			return nil
		}

		rel, _ := filepath.Rel(repo, path)
		name := d.Name()

		if isBuild(name) {
			found = append(found, rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}

		switch {
		case name == ".git", artifactNames[name]:
			return fs.SkipDir // build folders are shown as a whole anyway
		case strings.Count(rel, "/") >= maxArtifactDepth-1:
			return fs.SkipDir
		}
		if _, nested := KindOf(path); nested {
			return fs.SkipDir
		}
		return nil
	})

	return found
}

// Untracked keeps those of candidates that git does NOT track (new
// or ignored files). git ls-files prints tracked files;
// for a folder, the files inside it. On an error we assume everything is tracked
// and offer nothing: that is safer.
func Untracked(ctx context.Context, run shell.Runner, repo string, candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}

	args := append([]string{"-C", repo, "ls-files", "--"}, candidates...)
	out, err := run(ctx, Git, args...)
	if err != nil {
		return nil
	}

	tracked := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result []string
	for _, c := range candidates {
		isTracked := slices.ContainsFunc(tracked, func(t string) bool {
			return t == c || strings.HasPrefix(t, c+"/")
		})
		if !isTracked {
			result = append(result, c)
		}
	}
	return result
}

// Ignored keeps only those of candidates that git ignores.
// git check-ignore prints ignored paths and exits with code 1
// if there are none. Any error means we delete nothing: that is safer.
func Ignored(ctx context.Context, run shell.Runner, repo string, candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}

	args := append([]string{"-C", repo, "check-ignore", "--"}, candidates...)
	out, _ := run(ctx, Git, args...)

	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSuffix(strings.TrimSpace(line), "/")
		if slices.Contains(candidates, line) { // only what we asked about
			result = append(result, line)
		}
	}
	return result
}

// Branch is the branch name ("detached" if HEAD is not on a branch; "" if it could not be found out).
func Branch(ctx context.Context, run shell.Runner, repo string) string {
	out, err := run(ctx, Git, "-C", repo, "rev-parse", "--abbrev-ref", "HEAD")
	name := strings.TrimSpace(string(out))
	if err != nil || name == "" {
		return ""
	}
	if name == "HEAD" {
		return "detached"
	}
	return name
}

// LastCommit is the time of the last commit (zero time means no commits).
func LastCommit(ctx context.Context, run shell.Runner, repo string) time.Time {
	out, err := run(ctx, Git, "-C", repo, "log", "-1", "--format=%ct")
	if err != nil {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// notProjectFolders are folders right inside the home folder that are never
// project folders: macOS's own, and the ones macOS protects. Looking inside
// Desktop, Documents or Downloads would make macOS ask for access, and that
// must not happen just to make a suggestion.
var notProjectFolders = map[string]bool{
	"Applications": true, "Desktop": true, "Documents": true, "Downloads": true,
	"Library": true, "Movies": true, "Music": true, "Pictures": true, "Public": true,
}

// suggestDepth — how many levels below a folder to look for a repository
// when deciding whether it's a project folder.
const suggestDepth = 3

// ProjectFolders suggests project folders: the folders right inside home
// that have a git repository within a few levels (or are one). Names don't
// matter, so ~/my-projects and ~/work are found as well as ~/Projects.
// The result is sorted by name, because os.ReadDir sorts.
func ProjectFolders(home string) []string {
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil
	}

	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || notProjectFolders[name] {
			continue
		}
		if dir := filepath.Join(home, name); hasRepo(dir, suggestDepth) {
			out = append(out, dir)
		}
	}
	return out
}

// hasRepo — whether dir is a git repository or has one within depth levels.
// It stops at the first one it finds: one is enough to call it a project folder.
func hasRepo(dir string, depth int) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if rel != "." && (skipWhileSearching[d.Name()] || strings.HasPrefix(d.Name(), ".") ||
			strings.Count(rel, string(filepath.Separator)) >= depth) {
			return fs.SkipDir
		}
		if _, ok := KindOf(path); ok {
			found = true
			return fs.SkipAll // stop the whole walk
		}
		return nil
	})
	return found
}
