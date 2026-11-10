// Package core is the heart of the program: the shared concepts (size, item,
// category, action, provider) and the rules that apply to them.
//
// core imports nothing from the project. Every other package depends
// on it, not the other way round, so the core can be tested without a UI,
// without a disk and without external programs.
package core

import "fmt"

// Bytes is a size in bytes. A separate type instead of a bare int64
// (a value object): the compiler won't let you add bytes to something
// else by accident, and the formatting lives in the type itself.
type Bytes int64

// String turns a size into text like "104.3 GB".
// fmt calls this method by itself when it prints a Bytes via %s, %v or Println.
// We divide by 1000, like Finder, not by 1024, like df -h.
//
// The (b Bytes) before the name is the "receiver": the method belongs to
// the Bytes type and is called as size.String().
func (b Bytes) String() string {
	const unit = 1000

	if b < unit {
		// int64(b) converts back to a plain number explicitly, so that
		// fmt doesn't try to call Bytes.String() again and loop forever.
		return fmt.Sprintf("%d B", int64(b))
	}

	units := []string{"kB", "MB", "GB", "TB", "PB"} // a slice of strings, like an array in JS
	value := float64(b) / unit                      // a float, so we get 1.5 and not 1
	i := 0

	// Go has only for, there is no separate while. This is the "while":
	// keep dividing while the number is at least 1000 and there is a next unit.
	for value >= unit && i < len(units)-1 {
		value /= unit
		i++
	}

	// %.1f is a float with one digit after the decimal point.
	return fmt.Sprintf("%.1f %s", value, units[i])
}
