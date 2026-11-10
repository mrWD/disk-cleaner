package diskutil

import (
	"context"
	"io/fs"
	"path/filepath"
	"time"
)

// NewestFile is when the most recently changed file under path was changed:
// a good sign of when a program last used its folder. Zero if there are no
// files (or path doesn't exist). Like DirSize, it checks every few thousand
// files whether the scan was cancelled.
func NewestFile(ctx context.Context, path string) time.Time {
	var newest time.Time
	visited := 0
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // a folder we can't read: skip it, keep walking
		}
		visited++
		if visited%4096 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}
