package diskutil

import (
	"os"
	"path/filepath"
)

// VolumeName — имя системного диска, как его показывает Finder
// ("Macintosh HD"). В /Volumes у системного диска есть ссылка на "/" —
// её имя и есть название диска. Если найти не удалось — fallback.
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
