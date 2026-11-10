// Package diskutil is disk work: free space, folder sizes.
// This is infrastructure: it depends on core, not the other way round.
package diskutil

import (
	"syscall"

	"disk-cleaner/internal/core" // our own packages: module name + folder path
)

// FreeSpace returns the free and total space on the disk that holds path.
// A Go function can return several values. By convention the last one is the error.
//
// A name with a capital letter = the function is visible from other packages (exported).
// With a lowercase letter it is visible only inside diskutil. Go uses this instead of public/private.
func FreeSpace(path string) (core.Bytes, core.Bytes, error) {
	// An empty struct that the system will fill in.
	var st syscall.Statfs_t

	// &st is the struct's address: Statfs writes the result straight into it.
	err := syscall.Statfs(path, &st)
	if err != nil {
		// Errors in Go aren't thrown, they're returned. We check right away.
		return 0, 0, err
	}

	// The disk is divided into blocks of equal size.
	// Bavail is the free blocks available to an ordinary user,
	// Blocks is the total number of blocks, Bsize is the block size in bytes.
	// The fields have different types (uint64 and uint32), and Go doesn't convert
	// numbers by itself, so we convert everything to int64 explicitly.
	blockSize := int64(st.Bsize)
	free := core.Bytes(int64(st.Bavail) * blockSize)
	total := core.Bytes(int64(st.Blocks) * blockSize)

	return free, total, nil // nil means "no error"
}
