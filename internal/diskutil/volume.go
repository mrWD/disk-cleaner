package diskutil

import (
	"os"
	"path/filepath"
)

// VolumeName is the name of the system disk as Finder shows it
// ("Macintosh HD"). In /Volumes the system disk has a link to "/",
// and the link's name is the disk's name. If it can't be found, fallback is returned.
func VolumeName(fallback string) string {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return fallback
	}

	for _, e := range entries {
		target, err := os.Readlink(filepath.Join("/Volumes", e.Name()))
		if err == nil && target == "/" {
			return e.Name()
		}
	}
	return fallback
}
