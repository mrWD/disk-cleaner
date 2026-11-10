package core

// Look is how a provider looks in the list on the left: an icon and the
// colour of the tile behind it, like the sections in macOS System Settings.
//
// This is an optional interface: a provider may not implement it,
// and then the UI draws a default icon. The check in the UI looks like this:
//
//	if l, ok := p.(core.Look); ok { icon, color := l.Look() }
//
// The icon is a white SVG (drawn on top of the coloured tile),
// the colour is a string like "#007AFF". Plain data, not Fyne types,
// so that providers don't depend on the UI library.
type Look interface {
	Look() (icon []byte, color string)
}
