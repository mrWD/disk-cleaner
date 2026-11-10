// Package update tells whether GitHub has a newer version of Disk Cleaner
// than this copy, and installs it the way this copy was installed.
//
// There are two ways to have Disk Cleaner, and each updates its own way:
//
//   - Built from source (scripts/build.sh): a new version is a newer commit on
//     main. Go writes the commit an app was built from into the binary
//     (runtime/debug.ReadBuildInfo), and GitHub can say which commits came
//     after it. The update runs git pull and build.sh again, in Terminal.
//   - Downloaded from GitHub Releases (signed and notarised): a new version
//     is a newer release. The update downloads it, checks that it's signed
//     with the developer's certificate and accepted by Gatekeeper, puts it in
//     place of this copy and opens it.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"disk-cleaner/internal/shell"
)

// Repo is where new versions come from.
const Repo = "https://api.github.com/repos/mrWD/disk-cleaner"

// TeamID is the developer's Apple team: a downloaded update must be signed
// with this team's Developer ID certificate, or it isn't installed.
const TeamID = "742H5JJX37"

// AppAsset is the file the updater downloads from a release
// (packaging/sign_macos.sh makes it).
const AppAsset = "Disk-Cleaner.zip"

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

// errNotFound — GitHub answered 404.
var errNotFound = errors.New("not found on GitHub")

// Checker checks for and installs new versions.
type Checker struct {
	API        string                    // the GitHub API address of the repository (Repo)
	Branch     string                    // the branch new versions come from: "main"
	Build      Build                     // this copy
	AppVersion string                    // this copy's version, like "1.2.0" (FyneApp.toml or the release tag)
	SourceDir  string                    // the checkout this copy was built from; "" if downloaded
	Bundle     string                    // this copy's .app; "" when it isn't running from one (go run)
	Client     *http.Client              // with a timeout
	Git        shell.Runner              // runs git; tests replace it
	Open       func(path string) error   // opens the update script in Terminal; tests replace it
	Verify     func(app string) error    // checks a downloaded app's signature (verifyApp); tests replace it
	Start      func(script string) error // starts the swap script on its own (startDetached); tests replace it

	latest *release // the newest release, found by Check (downloaded copies)
}

// New — a checker for this copy of the app. appVersion is the version Fyne
// knows (app.Metadata().Version).
func New(appVersion string) *Checker {
	return &Checker{
		API:        Repo,
		Branch:     "main",
		Build:      Current(),
		AppVersion: appVersion,
		SourceDir:  SourceDir(),
		Bundle:     bundlePath(),
		Client:     &http.Client{Timeout: 60 * time.Second}, // a download of a few MB fits easily
		Git:        shell.Run,
		Open: func(path string) error {
			return exec.Command("/usr/bin/open", path).Run()
		},
		Verify: verifyApp,
		Start:  startDetached,
	}
}

// Version — the short commit of this copy, or "" if unknown.
func (c *Checker) Version() string { return c.Build.Short() }

// FromSource — whether this copy was built from source (and so updates with
// git pull and build.sh) rather than downloaded from GitHub Releases.
func (c *Checker) FromSource() bool { return c.SourceDir != "" || c.Bundle == "" }

// Check asks GitHub whether there's something newer than this copy: newer
// commits on main for a copy built from source, a newer release for a
// downloaded one.
//
// It sends a GET request or two to api.github.com, the same as opening the
// repository's page: nothing about the user or the Mac goes with them.
// Without a token GitHub allows 60 such requests an hour, plenty for one
// check at launch.
func (c *Checker) Check(ctx context.Context) (News, error) {
	if !c.FromSource() {
		return c.checkRelease(ctx)
	}
	if c.Build.Revision == "" || c.Build.Modified {
		return News{}, ErrUnknownBuild
	}
	news, err := c.compare(ctx, c.Branch)
	if errors.Is(err, errNotFound) {
		return News{}, ErrUnknownBuild // GitHub doesn't have this commit
	}
	return news, err
}

// get fetches a GitHub API address and decodes its JSON into out.
func (c *Checker) get(ctx context.Context, url string, out any) error {
	resp, err := c.request(ctx, url, "application/vnd.github+json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("reading GitHub's answer: %w", err)
	}
	return nil
}

// request sends a GET and checks the answer's status.
func (c *Checker) request(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "disk-cleaner") // GitHub refuses requests without one

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, errNotFound
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
}

// compare asks which commits head (a branch or a tag) has that this copy's
// commit doesn't.
func (c *Checker) compare(ctx context.Context, head string) (News, error) {
	// Only the fields we need. "ahead_by" — how many commits head has that
	// this copy doesn't; "commits" — those commits, oldest first.
	var body struct {
		AheadBy int `json:"ahead_by"`
		Commits []struct {
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		} `json:"commits"`
	}
	if err := c.get(ctx, fmt.Sprintf("%s/compare/%s...%s", c.API, c.Build.Revision, head), &body); err != nil {
		return News{}, err
	}

	news := News{Count: body.AheadBy}
	for i := len(body.Commits) - 1; i >= 0 && len(news.Titles) < maxTitles; i-- {
		title, _, _ := strings.Cut(body.Commits[i].Commit.Message, "\n")
		news.Titles = append(news.Titles, title)
	}
	return news, nil
}

// release — the parts of a GitHub release we use.
type release struct {
	Tag    string `json:"tag_name"` // like "v1.2.0"
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// checkRelease — for a downloaded copy: is the newest release newer than it?
// What's new is listed as the commits between this copy and that release.
func (c *Checker) checkRelease(ctx context.Context) (News, error) {
	var r release
	err := c.get(ctx, c.API+"/releases/latest", &r)
	if errors.Is(err, errNotFound) {
		return News{}, nil // no releases yet
	}
	if err != nil {
		return News{}, err
	}
	if !newer(strings.TrimPrefix(r.Tag, "v"), c.AppVersion) {
		return News{}, nil
	}
	c.latest = &r

	news, err := c.compare(ctx, r.Tag)
	if err != nil || news.Count == 0 {
		// The commits aren't essential: the release itself is the news.
		return News{Count: 1, Titles: []string{"Disk Cleaner " + strings.TrimPrefix(r.Tag, "v")}}, nil
	}
	return news, nil
}

// newer — whether version a ("1.10.0") is newer than b ("1.9.2"). Numbers
// are compared as numbers, part by part, so 1.10 comes after 1.9.
func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
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

// Install installs the new version. quit says whether the app must quit now
// so its replacement can be put in place (a downloaded copy); a copy built
// from source is closed by build.sh itself when the new one is ready.
func (c *Checker) Install(ctx context.Context) (quit bool, err error) {
	if !c.FromSource() {
		return true, c.installRelease(ctx)
	}
	return false, c.installSource(ctx)
}

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

// installSource writes a small script and opens it in Terminal: it pulls the
// new commits and runs scripts/build.sh --install, which closes this copy,
// puts the new one in /Applications and opens it.
//
// Why Terminal and not quietly in the background: the build takes a few
// minutes and may need to install Xcode Command Line Tools or Go, and if
// something goes wrong the error stays on screen.
func (c *Checker) installSource(ctx context.Context) error {
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

// installRelease downloads the newest release, checks it and starts the
// script that swaps it in once this copy has quit.
//
// The checks are the point: an app that replaces itself with something it
// downloaded must make sure that something comes from its developer. The
// new copy must have a valid signature from TeamID's Developer ID and be
// accepted by Gatekeeper (that is, notarised by Apple). If not, nothing changes.
func (c *Checker) installRelease(ctx context.Context) error {
	if c.latest == nil || c.Bundle == "" {
		return errors.New("no release to install: check for updates first")
	}
	var url string
	for _, a := range c.latest.Assets {
		if a.Name == AppAsset {
			url = a.URL
		}
	}
	if url == "" {
		return fmt.Errorf("the release %s has no %s", c.latest.Tag, AppAsset)
	}

	dir, err := os.MkdirTemp("", "disk-cleaner-update-")
	if err != nil {
		return err
	}
	zip := filepath.Join(dir, AppAsset)
	if err := c.download(ctx, url, zip); err != nil {
		return err
	}

	// ditto unpacks a .zip made by ditto keeping everything a signature needs.
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", "-x", "-k", zip, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("unpacking the update: %v: %s", err, out)
	}
	app := filepath.Join(dir, filepath.Base(c.Bundle))
	if err := c.Verify(app); err != nil {
		return fmt.Errorf("the downloaded update didn't pass the signature check, so it wasn't installed: %w", err)
	}

	script := filepath.Join(dir, "swap.sh")
	if err := os.WriteFile(script, []byte(SwapScript(os.Getpid(), c.Bundle, app)), 0o755); err != nil {
		return err
	}
	return c.Start(script)
}

// download saves url to path.
func (c *Checker) download(ctx context.Context, url, path string) error {
	resp, err := c.request(ctx, url, "application/octet-stream")
	if err != nil {
		return fmt.Errorf("downloading the update: %w", err)
	}
	defer resp.Body.Close()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("downloading the update: %w", err)
	}
	return f.Close()
}

// SwapScript waits for the app (process pid) to quit, puts the new copy in
// place of the old one and opens it. If the swap fails (no write access to
// /Applications, say), the old copy is put back and Finder shows the new one,
// so it can be dragged across by hand.
func SwapScript(pid int, old, fresh string) string {
	o, n, b := shellQuote(old), shellQuote(fresh), shellQuote(old+".old")
	return fmt.Sprintf(`#!/bin/sh
while kill -0 %d 2>/dev/null; do sleep 0.3; done
rm -rf %[3]s
if mv %[2]s %[3]s && mv %[4]s %[2]s; then
	rm -rf %[3]s
	open %[2]s
else
	[ -d %[3]s ] && [ ! -d %[2]s ] && mv %[3]s %[2]s
	open -R %[4]s
fi
`, pid, o, b, n)
}

// verifyApp checks a downloaded app: its signature is intact, it's signed by
// TeamID, and Gatekeeper accepts it (which needs Apple's notarisation).
func verifyApp(app string) error {
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
		return fmt.Errorf("signature: %v: %s", err, out)
	}
	// codesign -dv prints the signature's details to stderr.
	out, err := exec.Command("/usr/bin/codesign", "-dv", "--verbose=2", app).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "TeamIdentifier="+TeamID) {
		return fmt.Errorf("not signed by team %s", TeamID)
	}
	if out, err := exec.Command("/usr/sbin/spctl", "-a", "-t", "exec", app).CombinedOutput(); err != nil {
		return fmt.Errorf("Gatekeeper: %v: %s", err, out)
	}
	return nil
}

// startDetached runs a script in its own session, so it keeps running after
// this app quits (it has to: its job is to replace this app).
func startDetached(script string) error {
	cmd := exec.Command("/bin/sh", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start() // no Wait: we quit right after
}

// shellQuote puts s in single quotes for sh, so spaces and other special
// characters in a folder name can't break the command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SourceDir reads where this copy was built from: scripts/build.sh writes
// the checkout's path into the app bundle (Contents/Resources/source-dir.txt).
// "" when running without a bundle (go run) or from a downloaded release.
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

// bundlePath — the .app this copy runs from ("…/Disk Cleaner.app"), or ""
// when it runs as a bare binary (go run, tests).
func bundlePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	macos := filepath.Dir(exe) // …/Disk Cleaner.app/Contents/MacOS
	app := filepath.Dir(filepath.Dir(macos))
	if filepath.Base(macos) != "MacOS" || filepath.Ext(app) != ".app" {
		return ""
	}
	return app
}
