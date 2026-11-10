// Package shell runs external programs (docker, xcrun, git, ps).
//
// Despite the name, no command shell (zsh, bash) is involved here:
// the program is started directly and the arguments are passed as a list.
// So spaces, quotes and ; in the arguments can't "break" the command.
package shell

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// extraDirs is where to look for programs besides PATH.
//
// An app launched from Finder (not from a terminal) gets a trimmed-down
// PATH: /usr/bin:/bin:/usr/sbin:/sbin. Without these folders it wouldn't find
// brew, docker, go, or docker's registry-login helpers.
func extraDirs() []string {
	dirs := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/go/bin",
		"/Applications/Docker.app/Contents/Resources/bin",
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
		)
	}

	return dirs
}

// searchPath is the process's PATH plus extraDirs, without duplicates.
func searchPath() []string {
	seen := map[string]bool{}
	var out []string

	// filepath.SplitList splits the PATH string on colons.
	for _, dir := range append(filepath.SplitList(os.Getenv("PATH")), extraDirs()...) {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}

	return out
}

// LookPath finds a program by name and returns the full path to it.
// The second value tells whether the program was found.
func LookPath(name string) (string, bool) {
	if filepath.IsAbs(name) {
		info, err := os.Stat(name)
		return name, err == nil && !info.IsDir()
	}

	for _, dir := range searchPath() {
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		// Mode()&0o111 checks whether the file has the execute permission (the x bit).
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return full, true
		}
	}

	return "", false
}

// Env is the environment for the programs we start: the current one, but with the full PATH.
// docker, for example, looks in PATH for its own helper docker-credential-desktop.
func Env() []string {
	env := os.Environ()
	path := "PATH=" + strings.Join(searchPath(), string(os.PathListSeparator))

	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = path
			return env
		}
	}

	return append(env, path)
}

// Runner is a function that runs a program. Providers receive it from outside,
// so that tests can plug in a fake with prerecorded output
// and not depend on whether docker is installed on the machine.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Run starts a program and returns its standard output.
// If the program exits with an error, the error contains its stderr:
// otherwise it would be unclear why docker "crashed".
func Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, ok := LookPath(name)
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, exec.ErrNotFound)
	}

	// CommandContext kills the process if ctx is cancelled or its timeout expires.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = Env()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		// %w "wraps" the original error so that errors.Is/As can reach it.
		return stdout.Bytes(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
	}

	return stdout.Bytes(), nil
}
