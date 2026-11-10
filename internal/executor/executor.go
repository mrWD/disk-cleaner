// Package executor does the real work of running actions: deleting folders,
// running commands, asking for the administrator password.
//
// core holds only the core.Executor interface (a port), and here lives its
// implementation (an adapter). The core's tests get by with a fake and delete nothing.
//
// This is also the last line of defence: even if a buggy provider asks to
// delete the home folder or ~/Library, the executor will refuse.
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/shell"
)

// ErrUnsafePath means the path failed the safety check.
var ErrUnsafePath = errors.New("refusing to delete this path")

// protectedInHome lists folders inside the home folder that must never be
// deleted whole, under any circumstances: they hold settings, mail, passwords, iCloud, app data.
var protectedInHome = []string{
	"Library/Application Support",
	"Library/Caches",
	"Library/Containers",
	"Library/Developer",
	"Library/Group Containers",
	"Library/Keychains",
	"Library/Mail",
	"Library/Messages",
	"Library/Mobile Documents",
	"Library/Preferences",
	"Library/Android",
}

// Local runs actions on this computer.
type Local struct {
	// Home is the home folder. Without a password you can delete only inside it,
	// and at least two levels deeper: ~/.npm/_cacache is fine, ~/Projects is not.
	Home string

	// AdminRoots is where deleting with the administrator password is allowed
	// (the simulators' dyld cache belongs to root).
	AdminRoots []string

	// runAdmin runs a string in a shell with administrator rights.
	// It is a field, not a plain function, so that a test can swap in a fake
	// and avoid showing the password prompt.
	runAdmin func(ctx context.Context, shellCmd string) error

	// trash moves a path to the Trash. It is a field so that a test doesn't send
	// files to the user's real Trash.
	trash func(ctx context.Context, path string) error
}

// New creates an executor for the current user.
func New() (*Local, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	return &Local{
		Home:       home,
		AdminRoots: []string{"/Library/Developer/CoreSimulator/Caches"},
		runAdmin:   runWithAdminPrompt,
		trash:      moveToTrash,
	}, nil
}

// Run runs one action. A method with this name and signature
// makes *Local an implementation of the core.Executor interface.
func (e *Local) Run(ctx context.Context, a core.Action) error {
	switch {
	case a.RemovePath != "" && len(a.Command) > 0:
		return errors.New("action has both a path and a command")
	case a.RemovePath != "":
		return e.remove(ctx, a)
	case len(a.Command) > 0:
		return e.command(ctx, a)
	default:
		return errors.New("empty action")
	}
}

// CheckPath is protection against disaster. The path must be absolute, "clean"
// (no .. and no extra slashes) and lie where deleting is allowed.
func (e *Local) CheckPath(path string, admin bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%w: %q is not a clean absolute path", ErrUnsafePath, path)
	}

	if admin {
		for _, root := range e.AdminRoots {
			if isInside(path, root) {
				return nil
			}
		}
		return fmt.Errorf("%w: %s is outside %v", ErrUnsafePath, path, e.AdminRoots)
	}

	rel, err := filepath.Rel(e.Home, path) // the path relative to the home folder
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%w: %s is outside your home folder", ErrUnsafePath, path)
	}

	// Depth: "Projects" is 1 level (not allowed), ".npm/_cacache" is 2 (allowed).
	if len(strings.Split(rel, string(filepath.Separator))) < 2 {
		return fmt.Errorf("%w: %s is a top-level folder of your home", ErrUnsafePath, path)
	}

	for _, p := range protectedInHome {
		if rel == p {
			return fmt.Errorf("%w: %s holds data of other apps", ErrUnsafePath, path)
		}
	}

	return nil
}

// isInside tells whether path lies strictly inside root (not equal to it).
func isInside(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// remove deletes a file or a folder.
func (e *Local) remove(ctx context.Context, a core.Action) error {
	if err := e.CheckPath(a.RemovePath, a.NeedsAdmin); err != nil {
		return err
	}

	// Already gone means the goal is reached. errors.Is understands
	// "wrapped" errors too, so we compare with it and not with ==.
	if _, err := os.Lstat(a.RemovePath); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if a.Trash {
		return e.trash(ctx, a.RemovePath)
	}

	if a.NeedsAdmin {
		return e.runAdmin(ctx, "/bin/rm -rf "+singleQuote(a.RemovePath))
	}

	err := os.RemoveAll(a.RemovePath)
	if err != nil && errors.Is(err, fs.ErrPermission) {
		// RemoveAll can't delete read-only folders (that's how the Go module
		// cache is built). We give ourselves write permission and try again.
		makeWritable(a.RemovePath)
		err = os.RemoveAll(a.RemovePath)
	}

	return err
}

// makeWritable gives the owner write permission on all folders inside path.
func makeWritable(path string) {
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			if info, err := d.Info(); err == nil {
				_ = os.Chmod(p, info.Mode().Perm()|0o700) // 0o700 is rwx for the owner
			}
		}
		return nil
	})
}

// ErrAdminCommand: commands with administrator rights are forbidden. With the
// password we may only delete paths, and only inside AdminRoots. Otherwise a bug
// in a provider could run anything as root.
var ErrAdminCommand = errors.New("commands with administrator rights are not allowed")

// command runs a program.
func (e *Local) command(ctx context.Context, a core.Action) error {
	if a.NeedsAdmin {
		return ErrAdminCommand
	}

	// No shell: the arguments go to the program as they are.
	_, err := shell.Run(ctx, a.Command[0], a.Command[1:]...)
	return err
}

// singleQuote wraps a string in single quotes for the sh shell.
// Inside single quotes nothing is special ($, `, \) except the quote
// itself. So we replace each quote inside with four characters:
// close the quotes, an escaped quote, open the quotes again.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// moveToTrash moves a path to the Trash with the built-in macOS command
// /usr/bin/trash (no shell, the path is passed as it is).
func moveToTrash(ctx context.Context, path string) error {
	_, err := shell.Run(ctx, "/usr/bin/trash", path)
	return err
}

// runWithAdminPrompt runs a command with administrator rights through
// AppleScript: macOS itself shows the standard password prompt.
// The program never sees the password.
func runWithAdminPrompt(ctx context.Context, shellCmd string) error {
	// In an AppleScript string, \ and " must be escaped.
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(shellCmd)
	script := `do shell script "` + escaped + `" with administrator privileges`

	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
