package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// A sheet is a window-in-the-window for Settings and the first-launch setup:
// the main window dims, and a card in the middle holds a title, the content
// and a row of buttons.
//
// Fyne's own dialogs put padding around their content, so a scroll area
// inside one is inset: its top and bottom shadows stop short of the edges,
// and the scrollbar sits inside the padding, on top of the content. A sheet
// adds no padding of its own, so the content can scroll edge to edge.

// sheetLayout puts the dimming over the whole window and the card in the
// middle, at most maxW × maxH and never bigger than the window allows.
type sheetLayout struct{ maxW, maxH float32 }

func (l sheetLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(0, 0) }

func (l sheetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	dim, card := objects[0], objects[1]
	dim.Move(fyne.NewPos(0, 0))
	dim.Resize(size)

	// max(…, 0): Fyne's software painter crashes on negative sizes.
	w := max(min(l.maxW, size.Width-40), 0)
	h := max(min(l.maxH, size.Height-40), 0)
	card.Resize(fyne.NewSize(w, h))
	card.Move(fyne.NewPos((size.Width-w)/2, (size.Height-h)/2))
}

// showSheet shows body as a sheet titled title, with buttons centred at the
// bottom. It returns the function that closes it. While a sheet is shown,
// clicks only reach the sheet: Fyne sends input to the topmost overlay.
func (u *App) showSheet(title string, body fyne.CanvasObject, maxW, maxH float32, buttons ...fyne.CanvasObject) (close func()) {
	dim := canvas.NewRectangle(theme.Color(theme.ColorNameShadow))

	bg := canvas.NewRectangle(theme.Color(theme.ColorNameOverlayBackground))
	bg.CornerRadius = 12
	bg.StrokeColor = theme.Color(colorCardStroke)
	bg.StrokeWidth = 1

	head := container.NewVBox(pad(14, 10, 20, 20, text(title, theme.SizeNameSubHeadingText, theme.ColorNameForeground, true)), hairline())
	foot := container.NewVBox(hairline(), pad(12, 14, 20, 20, container.NewCenter(container.NewHBox(buttons...))))
	card := container.NewStack(bg, edges(head, foot, nil, nil, body))

	sheet := container.New(sheetLayout{maxW: maxW, maxH: maxH}, dim, card)
	overlays := u.win.Canvas().Overlays()
	overlays.Add(sheet)
	sheet.Resize(u.win.Canvas().Size()) // later window resizes reach overlays by themselves

	return func() { overlays.Remove(sheet) }
}

// sheetScroll wraps a sheet's content in a vertical scroll that runs edge to
// edge. The extra room on the right keeps the scrollbar off the content.
func sheetScroll(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVScroll(pad(12, 12, 20, 28, content))
}

// refreshOverlays re-lays out the sheets and dialogs shown over the window.
// Fyne doesn't do it by itself when something inside changes size (a list
// gets a row, a hidden label appears): without this the new content would
// overlap what's below it or stay squeezed to nothing.
func (u *App) refreshOverlays() {
	for _, o := range u.win.Canvas().Overlays().List() {
		o.Refresh()
	}
}
