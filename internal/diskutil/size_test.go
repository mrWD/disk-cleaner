package diskutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"disk-cleaner/internal/core"
)

// writeFile creates a file of the given size. t.Helper makes a failure
// point at the scenario's line, not at a line inside the helper.
func writeFile(t *testing.T, path string, size int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// make([]byte, size) is a slice of size zero bytes.
	// We fill it with 1s so that APFS doesn't make the file "sparse".
	data := make([]byte, size)
	for i := range data {
		data[i] = 1
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

const mb = 1_000_000

func TestDirSize(t *testing.T) {
	ctx := context.Background() // an empty context: no cancellation, no timeout

	t.Run("folder size is the sum of its files on disk", func(t *testing.T) {
		// Given: a folder with two 1 MB files (t.TempDir cleans itself up after the test)
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.bin"), mb)
		writeFile(t, filepath.Join(dir, "sub", "b.bin"), mb)

		// When: we measure the size
		size, err := DirSize(ctx, dir)

		// Then: about 2 MB (a bit more on disk because of blocks)
		if err != nil {
			t.Fatal(err)
		}
		if size < 2*mb || size > 2*mb+64_000 {
			t.Errorf("DirSize = %s, want about 2.0 MB", size)
		}
	})

	t.Run("hard links are counted once", func(t *testing.T) {
		// Given: a 1 MB file and a hard link to it (this is what pnpm does)
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.bin"), mb)
		if err := os.Link(filepath.Join(dir, "a.bin"), filepath.Join(dir, "link.bin")); err != nil {
			t.Fatal(err)
		}

		// When: we measure the size
		size, _ := DirSize(ctx, dir)

		// Then: 1 MB, not 2
		if size > mb+64_000 {
			t.Errorf("DirSize = %s, want about 1.0 MB", size)
		}
	})

	t.Run("symbolic links are not followed", func(t *testing.T) {
		// Given: a big folder outside and a link to it inside ours
		outside := t.TempDir()
		writeFile(t, filepath.Join(outside, "big.bin"), 3*mb)
		dir := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}

		// When: we measure our folder
		size, _ := DirSize(ctx, dir)

		// Then: the outsider's 3 MB were not counted
		if size >= mb {
			t.Errorf("DirSize = %s, the symlink target was counted", size)
		}
	})

	t.Run("missing path is an error", func(t *testing.T) {
		_, err := DirSize(ctx, filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Error("want an error for a missing path")
		}
	})

	t.Run("cancelled scan stops", func(t *testing.T) {
		// Given: a folder with lots of files and an already cancelled context
		dir := t.TempDir()
		for i := range 5000 { // range over a number: i = 0…4999
			writeFile(t, filepath.Join(dir, fmt.Sprintf("f%04d.bin", i)), 1)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		// When: we measure the size
		_, err := DirSize(cancelled, dir)

		// Then: we got a cancellation error
		if err == nil {
			t.Error("want context.Canceled, got nil")
		}
	})
}

func TestSizeAll(t *testing.T) {
	t.Run("sizes come back in the same order as paths", func(t *testing.T) {
		// Given: two folders of different sizes and one that doesn't exist
		small := t.TempDir()
		writeFile(t, filepath.Join(small, "a.bin"), mb)
		big := t.TempDir()
		writeFile(t, filepath.Join(big, "a.bin"), 3*mb)
		missing := filepath.Join(t.TempDir(), "nope")

		// When: we measure in parallel
		sizes := SizeAll(context.Background(), []string{big, missing, small}, 2)

		// Then: the order is preserved, and the missing one is 0
		if sizes[0] < 3*mb || sizes[1] != 0 || sizes[2] < mb || sizes[2] >= 2*mb {
			t.Errorf("SizeAll = %v", []core.Bytes(sizes))
		}
	})
}
