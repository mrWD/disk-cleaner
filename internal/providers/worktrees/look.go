package worktrees

import _ "embed" // _ means the package is only needed for the //go:embed below

// The icon for the list on the left: a white SVG drawn on top of a colored tile.
// //go:embed puts the contents of the file into the variable at build time.
//
//go:embed icon.svg
var icon []byte

// Look is the icon and the tile color (purple from the macOS system palette).
// Implements the optional core.Look interface.
func (p *Provider) Look() ([]byte, string) { return icon, "#AF52DE" }
