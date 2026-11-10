package gitscan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mkdir creates a folder (and all folders leading to it).
func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeGitFile makes dir a worktree: .git is a file pointing to .git/worktrees.
func writeGitFile(t *testing.T, dir, gitdir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindRepos(t *testing.T) {
	// Given: a main repository, a worktree inside it and a submodule
	root := t.TempDir()
	main := mkdir(t, root, "courses")
	mkdir(t, main, ".git")
	wt := mkdir(t, main, ".claude", "worktrees", "merge-main")
	writeGitFile(t, wt, main+"/.git/worktrees/merge-main")
	sub := mkdir(t, main, "vendor-lib")
	writeGitFile(t, sub, main+"/.git/modules/vendor-lib")

	// When
	repos := FindRepos(root)

	// Then: the main repository and the worktree are found, the submodule is not
	var got []string
	for _, r := range repos {
		kind := "main"
		if r.Kind == Worktree {
			kind = "worktree"
		}
		rel, _ := filepath.Rel(root, r.Path)
		got = append(got, kind+":"+rel)
	}
	want := []string{"main:courses", "worktree:courses/.claude/worktrees/merge-main"}
	if !slices.Equal(got, want) {
		t.Errorf("FindRepos = %v, want %v", got, want)
	}
}

func TestFindArtifactsStopsAtNestedRepos(t *testing.T) {
	// Given: a repository with its own node_modules and a worktree inside with someone else's
	root := t.TempDir()
	mkdir(t, root, ".git")
	mkdir(t, root, "node_modules", "react")
	mkdir(t, root, "ios", "Pods")
	wt := mkdir(t, root, ".claude", "worktrees", "x")
	writeGitFile(t, wt, root+"/.git/worktrees/x")
	mkdir(t, wt, "node_modules")

	// When
	found := FindArtifacts(root)

	// Then: only its own
	slices.Sort(found)
	if !slices.Equal(found, []string{"ios/Pods", "node_modules"}) {
		t.Errorf("FindArtifacts = %v", found)
	}
}

func TestFindBuilds(t *testing.T) {
	// Given: an archive and an installer in the root, an .apk inside build/ and an .app with nested .app
	root := t.TempDir()
	mkdir(t, root, ".git")
	mkdir(t, root, "Lingary-191.xcarchive", "Products")
	if err := os.WriteFile(filepath.Join(root, "Lingary-1.0.0.dmg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkdir(t, root, "android", "app", "build", "outputs", "app-debug.apk")
	mkdir(t, root, "desktop", "mac", "Lingary.app", "Contents", "Frameworks", "Helper.app")

	// When
	found := FindBuilds(root)
	slices.Sort(found)

	// Then: build/ is left alone (the folder is shown as a whole anyway),
	// we don't go inside the .app.
	want := []string{"Lingary-1.0.0.dmg", "Lingary-191.xcarchive", "desktop/mac/Lingary.app"}
	if !slices.Equal(found, want) {
		t.Errorf("FindBuilds = %v, want %v", found, want)
	}
}

func TestProjectFolders(t *testing.T) {
	// Given: a home folder with repositories in different places
	home := t.TempDir()
	for _, dir := range []string{
		"Projects/app/.git",            // the usual place
		"my-projects/client/site/.git", // any name, two levels down
		"dotfiles/.git",                // a folder that is a repository itself
		"work",                         // no repositories: not a project folder
		"deep/a/b/c/repo/.git",         // too deep to count
		"Documents/code/.git",          // macOS protects Documents: never looked into
		".hidden/repo/.git",            // hidden folders are skipped
		"Library/Developer/x/.git",     // macOS's own folder
	} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// When
	got := ProjectFolders(home)

	// Then: only the three real project folders, sorted by name
	want := []string{filepath.Join(home, "Projects"), filepath.Join(home, "dotfiles"), filepath.Join(home, "my-projects")}
	if !slices.Equal(got, want) {
		t.Errorf("ProjectFolders = %v, want %v", got, want)
	}
}
