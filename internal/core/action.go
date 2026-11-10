package core

import (
	"strconv"
	"strings"
)

// Action is one concrete cleaning step.
// Exactly one of the two is filled in: RemovePath or Command.
type Action struct {
	RemovePath string   // delete this path entirely
	Command    []string // or run a command: {"docker", "image", "rm", "abc"}
	NeedsAdmin bool     // needs the administrator password

	// Trash means: don't delete RemovePath for good, move it to the Trash.
	// We do this for the user's own files (for example from Downloads):
	// a mistake can be undone by taking the file back out of the Trash.
	Trash bool
}

// String shows the action the way it would look in a terminal.
// This is only for the confirmation window: the Executor runs commands
// directly, without a shell, so quotes and special characters are harmless there.
func (a Action) String() string {
	var parts []string // nil slice: append allocates the memory itself

	if a.NeedsAdmin {
		parts = append(parts, "sudo")
	}

	switch {
	case a.RemovePath != "" && a.Trash:
		parts = append(parts, "trash", shellQuote(a.RemovePath)) // /usr/bin/trash on macOS
	case a.RemovePath != "":
		parts = append(parts, "rm", "-rf", shellQuote(a.RemovePath))
	default:
		// range over a slice: _ is the index, arg is the value.
		for _, arg := range a.Command {
			parts = append(parts, shellQuote(arg))
		}
	}

	return strings.Join(parts, " ")
}

// shellQuote wraps a string in quotes only when a terminal would
// misread it without them (spaces, $, * and so on).
func shellQuote(s string) string {
	const special = " \t\n\"'\\$`!*?&;|<>(){}[]#~"
	if s != "" && !strings.ContainsAny(s, special) {
		return s
	}
	return strconv.Quote(s) // "string in quotes" with escaping
}
