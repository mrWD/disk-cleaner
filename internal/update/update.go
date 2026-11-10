// Package update tells whether GitHub has a newer version of Disk Cleaner
// than this copy, and installs it the way this copy was installed: from
// source, with git pull and scripts/build.sh --install.
//
// There are no version numbers to keep up to date. When Go builds the app
// inside a git checkout, it writes the commit into the binary on its own
// (runtime/debug.ReadBuildInfo), and GitHub can say which commits on main
// came after it. A new version is simply a newer commit on main.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"disk-cleaner/internal/shell"
)

// Repo is where new versions come from.
const Repo = "https://api.github.com/repos/mrWD/disk-cleaner"

// Build — what this copy of the app was built from.
type Build struct {
	Revision string    // the full commit hash; "" if unknown (for example, built outside git)
	Time     time.Time // when that commit was made
	Modified bool      // built from a folder with changes that weren't committed
}

// Current reads the build information Go wrote into this binary.
func Current() Build {
	var b Build
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Revision = s.Value
		case "vcs.time":
			b.Time, _ = time.Parse(time.RFC3339, s.Value)
		case "vcs.modified":
			b.Modified = s.Value == "true"
		}
	}
	return b
}

// Short is the commit as GitHub shows it: its first seven characters.
func (b Build) Short() string {
	if len(b.Revision) > 7 {
		return b.Revision[:7]
	}
	return b.Revision
}

// News — what GitHub has that this copy doesn't.
type News struct {
	Count  int      // how many new commits
	Titles []string // their first lines, newest first, at most maxTitles
}

const maxTitles = 5

// ErrUnknownBuild — this copy can't be compared with GitHub: it was built
// from uncommitted changes, outside git, or from a commit GitHub doesn't
// have (pushed nowhere yet). That's a developer's own build, and telling the
// developer about "new versions" of their own work would only be noise.
var ErrUnknownBuild = errors.New("this build can't be compared with GitHub")

// Checker checks for and installs new versions.
type Checker struct {
	API       string                  // the GitHub API address of the repository (Repo)
	Branch    string                  // the branch new versions come from: "main"
	Build     Build                   // this copy
	SourceDir string                  // the checkout this copy was built from; "" if unknown
	Client    *http.Client            // with a timeout
	Git       shell.Runner            // runs git; tests replace it
	Open      func(path string) error // opens the update script in Terminal; tests replace it
}

// New — a checker for this copy of the app.
func New() *Checker {
	return &Checker{
		API:       Repo,
		Branch:    "main",
		Build:     Current(),
		SourceDir: SourceDir(),
		Client:    &http.Client{Timeout: 15 * time.Second},
		Git:       shell.Run,
		Open: func(path string) error {
			return exec.Command("/usr/bin/open", path).Run()
		},
	}
}

// Version — the short commit of this copy, or "" if unknown.
func (c *Checker) Version() string { return c.Build.Short() }

// Check asks GitHub which commits on the branch came after this copy's.
//
// It sends one GET request to api.github.com, the same as opening the
// repository's page: nothing about the user or the Mac goes with it.
// Without a token GitHub allows 60 such requests an hour, plenty for one
// check at launch.
func (c *Checker) Check(ctx context.Context) (News, error) {
	if c.Build.Revision == "" || c.Build.Modified {
		return News{}, ErrUnknownBuild
	}

	url := fmt.Sprintf("%s/compare/%s...%s", c.API, c.Build.Revision, c.Branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return News{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "disk-cleaner") // GitHub refuses requests without one

	resp, err := c.Client.Do(req)
	if err != nil {
		return News{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return News{}, ErrUnknownBuild // GitHub doesn't have this commit
	default:
		return News{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}

	// Only the fields we need. "ahead_by" — how many commits the branch has
	// that this copy doesn't; "commits" — those commits, oldest first.
	var body struct {
		AheadBy int `json:"ahead_by"`
		Commits []struct {
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return News{}, fmt.Errorf("reading GitHub's answer: %w", err)
	}

	news := News{Count: body.AheadBy}
	for i := len(body.Commits) - 1; i >= 0 && len(news.Titles) < maxTitles; i-- {
		title, _, _ := strings.Cut(body.Commits[i].Commit.Message, "\n")
		news.Titles = append(news.Titles, title)
	}
	return news, nil
}

// Blocked — why the update can't be installed automatically. Key is a text
// key for the window; Dir is the checkout it's about.
type Blocked struct {
	Key string
	Dir string
}

func (b *Blocked) Error() string { return b.Key + ": " + b.Dir }

// Reasons an update can't be installed automatically (texts in the UI translations).
const (
	BlockedNoSource = "update.no_source"     // the checkout is unknown or gone
	BlockedChanges  = "update.local_changes" // the checkout has changes of its own
	BlockedBranch   = "update.not_main"      // the checkout is on another branch
)

// Prepare checks that the checkout can be updated without surprises: it
// exists, has no changes of its own, and is on the branch updates come
// from. Otherwise git pull could fail halfway or mix the update into
// someone's work, so the user is told to update by hand instead.
func (c *Checker) Prepare(ctx context.Context) error {
	dir := c.SourceDir
	if dir == "" {
		return &Blocked{Key: BlockedNoSource}
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return &Blocked{Key: BlockedNoSource, Dir: dir}
	}

	out, err := c.Git(ctx, "git", "-C", dir, "status", "--porcelain")
	if err != nil {
		return &Blocked{Key: BlockedNoSource, Dir: dir}
	}
	if strings.TrimSpace(string(out)) != "" {
		return &Blocked{Key: BlockedChanges, Dir: dir}
	}

	out, err = c.Git(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(string(out)) != c.Branch {
		return &Blocked{Key: BlockedBranch, Dir: dir}
	}
	return nil
}

// Install writes a small script and opens it in Terminal: it pulls the new
// commits and runs scripts/build.sh --install, which closes this copy,
// puts the new one in /Applications and opens it.
//
// Why Terminal and not quietly in the background: the build takes a few
// minutes and may need to install Xcode Command Line Tools or Go, and if
// something goes wrong the error stays on screen. And a process this app
// started itself would be killed along with it when build.sh closes the app.
func (c *Checker) Install(ctx context.Context) error {
	if err := c.Prepare(ctx); err != nil {
		return err
	}

	f, err := os.CreateTemp("", "disk-cleaner-update-*.command")
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(Script(c.SourceDir)); err != nil {
		return err
	}
	if err := f.Chmod(0o755); err != nil { // a .command file must be executable to run in Terminal
		return err
	}
	return c.Open(f.Name())
}

// Script — the update script for the checkout in dir. --ff-only: take the
// new commits only if they simply follow the local ones, never merge.
func Script(dir string) string {
	return fmt.Sprintf(`#!/bin/sh
cd %s || exit 1
echo "Updating Disk Cleaner in $(pwd)"
echo
if git pull --ff-only && ./scripts/build.sh --install; then
	echo
	echo "Disk Cleaner is up to date. You can close this window."
else
	echo
	echo "The update didn't finish; the reason is above. Your installed copy is unchanged."
fi
`, shellQuote(dir))
}

// shellQuote puts s in single quotes for sh, so spaces and other special
// characters in a folder name can't break the command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SourceDir reads where this copy was built from: scripts/build.sh writes
// the checkout's path into the app bundle (Contents/Resources/source-dir.txt).
// "" when running without a bundle, for example with go run.
func SourceDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "..", "Resources", "source-dir.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
