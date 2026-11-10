package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"disk-cleaner/internal/shell"
)

// github — a fake GitHub API that answers the compare request with status and body.
func github(t *testing.T, status int, body string) *Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/compare/abc1234def...main") {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("GitHub needs a User-Agent")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Checker{API: srv.URL, Branch: "main", Build: Build{Revision: "abc1234def"}, Client: srv.Client()}
}

func TestCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("new commits on main are news, newest first", func(t *testing.T) {
		c := github(t, 200, `{"status":"ahead","ahead_by":2,"commits":[
			{"commit":{"message":"Older change\n\nDetails"}},
			{"commit":{"message":"Newest change"}}]}`)

		news, err := c.Check(ctx)

		if err != nil || news.Count != 2 || !slices.Equal(news.Titles, []string{"Newest change", "Older change"}) {
			t.Errorf("news = %+v, err = %v", news, err)
		}
	})

	t.Run("nothing new", func(t *testing.T) {
		c := github(t, 200, `{"status":"identical","ahead_by":0,"commits":[]}`)

		news, err := c.Check(ctx)

		if err != nil || news.Count != 0 {
			t.Errorf("news = %+v, err = %v", news, err)
		}
	})

	t.Run("a commit GitHub doesn't have is a developer's own build", func(t *testing.T) {
		c := github(t, 404, `{"message":"Not Found"}`)

		if _, err := c.Check(ctx); !errors.Is(err, ErrUnknownBuild) {
			t.Errorf("err = %v, want ErrUnknownBuild", err)
		}
	})

	t.Run("a build with uncommitted changes isn't compared at all", func(t *testing.T) {
		c := &Checker{Build: Build{Revision: "abc1234def", Modified: true}} // no server: no request may be made

		if _, err := c.Check(ctx); !errors.Is(err, ErrUnknownBuild) {
			t.Errorf("err = %v, want ErrUnknownBuild", err)
		}
	})
}

// checkout makes a real git repository on branch main with one commit.
func checkout(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "first"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestInstall(t *testing.T) {
	ctx := context.Background()

	t.Run("a clean checkout on main gets the script opened in Terminal", func(t *testing.T) {
		dir := checkout(t)
		var opened string
		c := &Checker{Branch: "main", SourceDir: dir, Git: shell.Run, Open: func(p string) error { opened = p; return nil }}

		if quit, err := c.Install(ctx); err != nil || quit {
			t.Fatalf("quit = %v, err = %v; a source update doesn't quit the app itself", quit, err)
		}

		script, _ := os.ReadFile(opened)
		if !strings.HasSuffix(opened, ".command") || !strings.Contains(string(script), "git pull --ff-only && ./scripts/build.sh --install") ||
			!strings.Contains(string(script), "cd '"+dir+"'") {
			t.Errorf("opened %s:\n%s", opened, script)
		}
	})

	t.Run("local changes, another branch or no checkout: hands off", func(t *testing.T) {
		changed := checkout(t)
		if err := os.WriteFile(filepath.Join(changed, "mine.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		branch := checkout(t)
		if out, err := exec.Command("git", "-C", branch, "checkout", "-q", "-b", "feature").CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}

		for dir, want := range map[string]string{
			changed:                               BlockedChanges,
			branch:                                BlockedBranch,
			filepath.Join(t.TempDir(), "missing"): BlockedNoSource,
			"":                                    BlockedNoSource,
		} {
			c := &Checker{Branch: "main", SourceDir: dir, Git: shell.Run, Open: func(string) error {
				t.Errorf("%q: Terminal must not be opened", dir)
				return nil
			}}
			var blocked *Blocked
			if _, err := c.Install(ctx); !errors.As(err, &blocked) || blocked.Key != want {
				t.Errorf("%q: err = %v, want %s", dir, err, want)
			}
		}
	})

	t.Run("odd folder names can't break the script", func(t *testing.T) {
		if got := shellQuote("/Users/me/it's here"); got != `'/Users/me/it'\''s here'` {
			t.Errorf("shellQuote = %s", got)
		}
	})
}

func TestNewer(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"1.2.1", "1.2.0", true},
		{"1.10.0", "1.9.2", true}, // numbers, not text
		{"2.0", "1.99.99", true},
		{"1.2.0", "1.2.0", false},
		{"1.2.0", "1.3.0", false},
		{"1.2", "1.2.0", false},
	} {
		if got := newer(tt.a, tt.b); got != tt.want {
			t.Errorf("newer(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// releases — a fake GitHub with one release, v1.3.0, whose Disk-Cleaner.zip
// holds an app made by ditto, like the real one.
func releases(t *testing.T) (*Checker, *httptest.Server) {
	t.Helper()
	stage := t.TempDir()
	app := filepath.Join(stage, "Disk Cleaner.app", "Contents", "MacOS")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(t.TempDir(), AppAsset)
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", "--keepParent", filepath.Join(stage, "Disk Cleaner.app"), zip).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.3.0","assets":[{"name":"Disk-Cleaner.zip","browser_download_url":"` + srv.URL + `/download/Disk-Cleaner.zip"}]}`))
		case r.URL.Path == "/compare/abc1234def...v1.3.0":
			_, _ = w.Write([]byte(`{"ahead_by":1,"commits":[{"commit":{"message":"Update checks"}}]}`))
		case r.URL.Path == "/download/Disk-Cleaner.zip":
			http.ServeFile(w, r, zip)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := &Checker{
		API: srv.URL, Branch: "main", Client: srv.Client(),
		Build:      Build{Revision: "abc1234def"},
		AppVersion: "1.2.0",
		Bundle:     "/Applications/Disk Cleaner.app", // a downloaded copy: no SourceDir
	}
	return c, srv
}

func TestReleases(t *testing.T) {
	ctx := context.Background()

	t.Run("a downloaded copy is told about a newer release and what's in it", func(t *testing.T) {
		c, _ := releases(t)

		news, err := c.Check(ctx)

		if err != nil || news.Count != 1 || len(news.Titles) != 1 || news.Titles[0] != "Update checks" {
			t.Errorf("news = %+v, err = %v", news, err)
		}
	})

	t.Run("the same or an older release is no news", func(t *testing.T) {
		c, _ := releases(t)
		c.AppVersion = "1.3.0"

		if news, err := c.Check(ctx); err != nil || news.Count != 0 {
			t.Errorf("news = %+v, err = %v", news, err)
		}
	})

	t.Run("a verified download is swapped in after the app quits", func(t *testing.T) {
		c, _ := releases(t)
		var verified, started string
		c.Verify = func(app string) error { verified = app; return nil }
		c.Start = func(script string) error { started = script; return nil }
		if _, err := c.Check(ctx); err != nil {
			t.Fatal(err)
		}

		quit, err := c.Install(ctx)

		if err != nil || !quit {
			t.Fatalf("quit = %v, err = %v", quit, err)
		}
		if filepath.Base(verified) != "Disk Cleaner.app" {
			t.Errorf("verified %q, want the unpacked app", verified)
		}
		script, _ := os.ReadFile(started)
		if !strings.Contains(string(script), "mv '"+verified+"' '/Applications/Disk Cleaner.app'") {
			t.Errorf("swap script:\n%s", script)
		}
	})

	t.Run("a download that fails the signature check is never installed", func(t *testing.T) {
		c, _ := releases(t)
		c.Verify = func(string) error { return errors.New("not signed by team " + TeamID) }
		c.Start = func(string) error {
			t.Error("the swap must not start")
			return nil
		}
		if _, err := c.Check(ctx); err != nil {
			t.Fatal(err)
		}

		if _, err := c.Install(ctx); err == nil || !strings.Contains(err.Error(), "signature check") {
			t.Errorf("err = %v", err)
		}
	})
}
