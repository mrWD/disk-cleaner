// Package access checks macOS privacy permissions for folders (the ones in
// System Settings › Privacy & Security › Files and Folders) and opens that
// page of System Settings.
//
// An app can't grant or take back these permissions itself: only the user
// can, in System Settings. What an app can do is ask (macOS shows its dialog
// the first time the app reads a protected folder, such as Downloads) and
// check whether it has access.
package access

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
)

// Status — whether macOS lets the app read a folder.
type Status int

const (
	Granted Status = iota // the folder can be read
	Denied                // macOS said no, or the user hasn't allowed it yet
)

// Folder reads one entry of dir. If macOS hasn't asked about this folder
// yet, this read is what makes it ask, and it waits for the answer, so call
// it off the UI thread. A folder that doesn't exist counts as Granted:
// there is nothing to be denied.
func Folder(dir string) Status {
	f, err := os.Open(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Granted
	}
	if err != nil {
		return Denied
	}
	defer f.Close()

	// Opening a protected folder can succeed; reading what's inside is what
	// macOS checks. An empty folder answers io.EOF, which is fine.
	if _, err := f.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return Denied
	}
	return Granted
}

// filesAndFolders — the address of System Settings › Privacy & Security ›
// Files and Folders.
const filesAndFolders = "x-apple.systempreferences:com.apple.preference.security?Privacy_FilesAndFolders"

// OpenSettings opens that page, where the user turns access on or off.
func OpenSettings() error {
	return exec.Command("/usr/bin/open", filesAndFolders).Run()
}
