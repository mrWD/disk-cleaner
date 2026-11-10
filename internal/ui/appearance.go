package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// The appearance choice in settings is three tiles with pictures, like
// System Settings in macOS (Appearance): "Light", "Dark", "Auto".
// Each tile shows a small window on a desktop background in the right colours;
// for "Auto" the left half is light and the right half is dark.

// The size of the picture on a tile.
const (
	previewW = 104
	previewH = 66
)

// The colour of the "desktop" in the picture: light and dark.
var (
	wallpaperLight = hex("#A9C1E3")
	wallpaperDark  = hex("#22304D")
)

// appearancePicker is the three tiles and the current choice.
type appearancePicker struct {
	u        *App
	selected string
	box      *fyne.Container // tiles are put here; rebuilt when the choice changes
}

// newAppearancePicker makes the tiles with current already selected.
func (u *App) newAppearancePicker(current string) *appearancePicker {
	p := &appearancePicker{u: u, selected: current, box: container.NewHBox()}
	p.refill()
	return p
}

// pick selects an option (a click on a tile).
func (p *appearancePicker) pick(mode string) {
	p.selected = mode
	p.refill()
}

// refill rebuilds the tiles so that the selected one gets the border.
func (p *appearancePicker) refill() {
	options := []struct {
		mode, labelKey string
		left, right    palette
		wallL, wallR   color.Color
	}{
		{appearanceLight, "settings.appearance.light", lightPalette, lightPalette, wallpaperLight, wallpaperLight},
		{appearanceDark, "settings.appearance.dark", darkPalette, darkPalette, wallpaperDark, wallpaperDark},
		{appearanceSystem, "settings.appearance.system", lightPalette, darkPalette, wallpaperLight, wallpaperDark},
	}

	p.box.Objects = nil
	for _, o := range options {
		mode := o.mode // our own copy for the closure below
		selected := mode == p.selected

		// The border of the selected tile is blue, the others have a transparent one.
		frame := canvas.NewRectangle(color.Transparent)
		frame.CornerRadius = 10
		frame.StrokeWidth = 2.5
		frame.StrokeColor = color.Transparent
		if selected {
			frame.StrokeColor = theme.Color(theme.ColorNamePrimary)
		}

		picture := container.NewStack(frame, pad(4, 4, 4, 4,
			container.NewGridWrap(fyne.NewSize(previewW, previewH), preview(o.left, o.right, o.wallL, o.wallR))))

		label := text(p.u.t(o.labelKey), theme.SizeNameText, theme.ColorNameForeground, selected)
		tile := container.NewVBox(picture, container.NewCenter(label))

		p.box.Add(newTappable(pad(2, 4, 4, 4, tile), func() { p.pick(mode) }))
	}
	p.box.Refresh()
}

// preview draws a small window on a desktop background. left and right are
// the colours of the left and right halves (they differ for "Auto").
//
// container.NewWithoutLayout is a container with no layout: we place every
// rectangle by hand with Move (where) and Resize (what size).
func preview(left, right palette, wallL, wallR color.Color) fyne.CanvasObject {
	const (
		w, h  = float32(previewW), float32(previewH)
		half  = w / 2
		winX  = 14 // window: left margin
		winY  = 12 // and top margin; at the bottom the window runs off the edge, as in macOS
		winR  = w - 10
		sideW = 22 // the window's sidebar
	)

	var objs []fyne.CanvasObject
	place := func(c color.Color, x, y, width, height float32) *canvas.Rectangle {
		r := canvas.NewRectangle(c)
		r.Move(fyne.NewPos(x, y))
		r.Resize(fyne.NewSize(width, height))
		objs = append(objs, r)
		return r
	}

	// Desktop: two halves, only the outer corners are rounded.
	l := place(wallL, 0, 0, half, h)
	l.TopLeftCornerRadius, l.BottomLeftCornerRadius = 7, 7
	r := place(wallR, half, 0, half, h)
	r.TopRightCornerRadius, r.BottomRightCornerRadius = 7, 7

	// Window: the left part in the left colours, the right part in the right colours.
	wl := place(left.background, winX, winY, half-winX, h-winY)
	wl.TopLeftCornerRadius = 4
	wr := place(right.background, half, winY, winR-half, h-winY)
	wr.TopRightCornerRadius = 4

	// The window's sidebar (in the left half) and the blue "selected row".
	place(left.sidebar, winX, winY+3, sideW, h-winY-3)
	place(left.accent, winX+3, winY+8, sideW-6, 4)

	// Two "text lines" in the window, one half-line in each part.
	textX := float32(winX + sideW + 6)
	place(left.secondary, textX, winY+9, half-textX, 3)
	place(right.secondary, half, winY+9, winR-half-10, 3)
	place(left.tertiary, textX, winY+17, half-textX, 3)
	place(right.tertiary, half, winY+17, winR-half-22, 3)

	return container.NewWithoutLayout(objs...)
}
