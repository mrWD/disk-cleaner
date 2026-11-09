package gitscan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mkdir создаёт папку (и все папки до неё).
func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeGitFile делает dir worktree: .git — файл со ссылкой на .git/worktrees.
func writeGitFile(t *testing.T, dir, gitdir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindRepos(t *testing.T) {
	// Given: основной репозиторий, worktree внутри него и подмодуль
	root := t.TempDir()
	main := mkdir(t, root, "courses")
	mkdir(t, main, ".git")
	wt := mkdir(t, main, ".claude", "worktrees", "merge-main")
	writeGitFile(t, wt, main+"/.git/worktrees/merge-main")
	sub := mkdir(t, main, "vendor-lib")
	writeGitFile(t, sub, main+"/.git/modules/vendor-lib")

	// When
	repos := FindRepos(root)

	// Then: основной и worktree найдены, подмодуль — нет
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
	// Given: репозиторий со своими node_modules и worktree внутри с чужими
	root := t.TempDir()
	mkdir(t, root, ".git")
	mkdir(t, root, "node_modules", "react")
	mkdir(t, root, "ios", "Pods")
	wt := mkdir(t, root, ".claude", "worktrees", "x")
	writeGitFile(t, wt, root+"/.git/worktrees/x")
	mkdir(t, wt, "node_modules")

	// When
	found := FindArtifacts(root)

	// Then: только свои
	slices.Sort(found)
	if !slices.Equal(found, []string{"ios/Pods", "node_modules"}) {
		t.Errorf("FindArtifacts = %v", found)
	}
}

func TestFindBuilds(t *testing.T) {
	// Given: архив и установщик в корне, .apk внутри build/ и .app с вложенными .app
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

	// Then: build/ не трогаем (папка и так показывается целиком),
	// внутрь .app не заходим.
	want := []string{"Lingary-1.0.0.dmg", "Lingary-191.xcarchive", "desktop/mac/Lingary.app"}
	if !slices.Equal(found, want) {
		t.Errorf("FindBuilds = %v, want %v", found, want)
	}
}
