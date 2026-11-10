package access

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFolder(t *testing.T) {
	t.Run("a readable folder is granted, even an empty one", func(t *testing.T) {
		dir := t.TempDir()
		if got := Folder(dir); got != Granted {
			t.Errorf("empty folder: %v, want Granted", got)
		}

		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Folder(dir); got != Granted {
			t.Errorf("folder with a file: %v, want Granted", got)
		}
	})

	t.Run("a folder that can't be read is denied", func(t *testing.T) {
		// Given: a folder without read permission, like one macOS protects
		dir := filepath.Join(t.TempDir(), "locked")
		if err := os.Mkdir(dir, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // so the test can remove it

		if got := Folder(dir); got != Denied {
			t.Errorf("locked folder: %v, want Denied", got)
		}
	})

	t.Run("a missing folder has nothing to deny", func(t *testing.T) {
		if got := Folder(filepath.Join(t.TempDir(), "missing")); got != Granted {
			t.Errorf("missing folder: %v, want Granted", got)
		}
	})
}
