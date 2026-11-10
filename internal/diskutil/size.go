package diskutil

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"disk-cleaner/internal/core"
)

// DirSize is how much disk space path (a folder or a file) takes up.
//
// What matters:
//   - The disk blocks actually used are counted (like du does), not the "logical"
//     size of the files: that is exactly how much space will be freed.
//   - The walk doesn't cross into other file systems. Otherwise, as with du
//     on 08.11.2026, the size would include the contents of mounted images
//     or a connected iPhone (DeviceFS showed a phantom 60 GB).
//   - Symbolic links are not followed: a link is just the link itself.
//   - Hard links to one file (pnpm makes them) are counted once.
//   - Folders that can't be read are skipped.
func DirSize(ctx context.Context, path string) (core.Bytes, error) {
	// Lstat, not Stat: if path is a link, we take the link itself, not its target.
	rootInfo, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}

	// info.Sys() is the system's "raw" data. .(*syscall.Stat_t) is a type
	// check: "if there is a Stat_t inside, give it to me, ok = true".
	rootStat, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return core.Bytes(rootInfo.Size()), nil
	}
	device := rootStat.Dev // number of the file system that path lives on

	// Key for tracking hard links: a file is uniquely identified
	// by the device number and the inode number.
	type inode struct {
		dev int64
		ino uint64
	}
	seen := map[inode]bool{}

	var total int64
	visited := 0

	// WalkDir walks the tree and calls our function for every entry.
	walkErr := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// No access to the folder, or the file vanished during the walk:
			// we skip this entry but keep walking.
			return nil
		}

		// Once every few thousand files we check whether the scan was cancelled.
		visited++
		if visited%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return err // any error returned from the function stops WalkDir
			}
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			total += info.Size()
			return nil
		}

		// A different file system: we don't go in there.
		if st.Dev != device {
			if d.IsDir() {
				return fs.SkipDir // a special "error": skip the whole folder
			}
			return nil
		}

		// A file with several hard links is counted only the first time we meet it.
		if !d.IsDir() && st.Nlink > 1 {
			key := inode{dev: int64(st.Dev), ino: st.Ino}
			if seen[key] {
				return nil
			}
			seen[key] = true
		}

		// Blocks is how many 512-byte blocks are used on disk.
		total += st.Blocks * 512
		return nil
	})

	if walkErr != nil {
		return core.Bytes(total), walkErr
	}

	return core.Bytes(total), nil
}

// SizeAll measures several paths in parallel and returns the sizes
// in the same order. If a path can't be measured, its size is 0.
//
// How it works (goroutines are Go's lightweight threads):
//   - we start a goroutine for each path (go func() {...}());
//   - the sem channel of size workers acts as a "turnstile": no more than
//     workers goroutines measure at the same time, so the disk isn't swamped;
//   - the WaitGroup waits until all the goroutines are done.
func SizeAll(ctx context.Context, paths []string, workers int) []core.Bytes {
	if workers < 1 {
		workers = 1
	}

	sizes := make([]core.Bytes, len(paths))
	sem := make(chan struct{}, workers) // a buffered channel with room for workers entries
	var wg sync.WaitGroup

	for i, p := range paths {
		wg.Add(1)

		go func() {
			defer wg.Done() // defer runs when the function exits, even on an error

			sem <- struct{}{}        // take a slot (wait if all are taken)
			defer func() { <-sem }() // release the slot at the end

			size, err := DirSize(ctx, p)
			if err == nil {
				// Each goroutine writes only to its own cell sizes[i],
				// so they don't get in each other's way.
				sizes[i] = size
			}
		}()
	}

	wg.Wait()
	return sizes
}
