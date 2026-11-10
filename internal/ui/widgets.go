package ui

import (
	"fmt"
	"hash/crc32"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Styling building blocks in the spirit of macOS: cards, "pills", section icons,
// thin dividers. All the screens are assembled from them.
//
// canvas.* is "drawing": a rectangle, text, an image. They can't do
// anything except look a certain way. widget.* are interactive elements.

// card is a "grouped" card like in System Settings:
// a white (grey in the dark look) rounded rectangle with a thin border.
func card(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(colorCard))
	bg.CornerRadius = 10
	bg.StrokeColor = theme.Color(colorCardStroke)
	bg.StrokeWidth = 1

	// Stack puts objects on top of each other: the background below, the content above.
	return container.NewStack(bg, pad(10, 10, 14, 14, content))
}

// text is a label of the needed size, colour and weight. Unlike Label,
// it does not wrap by words, so it is for short titles and numbers.
func text(s string, size fyne.ThemeSizeName, colorName fyne.ThemeColorName, bold bool) *canvas.Text {
	t := canvas.NewText(s, theme.Color(colorName))
	t.TextSize = theme.Size(size)
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}

// secondary is grey text that wraps by words (or is cut off with "…"
// if wrap = false). RichText supports both its own colour and wrapping.
func secondary(s string, wrap bool) *widget.RichText {
	rt := widget.NewRichText(&widget.TextSegment{
		Text: s,
		Style: widget.RichTextStyle{
			ColorName: colorSecondary,
			SizeName:  sizeCaption,
			Inline:    true,
		},
	})
	if wrap {
		rt.Wrapping = fyne.TextWrapWord
	} else {
		rt.Truncation = fyne.TextTruncateEllipsis
	}
	return rt
}

// colored is text of a given colour with wrapping (for warnings).
func colored(s string, colorName fyne.ThemeColorName) *widget.RichText {
	rt := widget.NewRichText(&widget.TextSegment{
		Text:  s,
		Style: widget.RichTextStyle{ColorName: colorName, SizeName: sizeCaption, Inline: true},
	})
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// pill is a "pill" like the statuses in macOS: coloured text on a pale
// background of the same colour, with rounded edges.
func pill(s string, colorName fyne.ThemeColorName) fyne.CanvasObject {
	c := theme.Color(colorName)

	label := text(s, theme.SizeNameCaptionText, colorName, true)
	bg := canvas.NewRectangle(withAlpha(c, 0x2E)) // ~18% opacity
	bg.CornerRadius = 8

	return container.NewCenter( // Center: don't stretch the pill to the height of the row
		container.NewStack(bg, pad(2, 2, 8, 8, label)),
	)
}

// padLayout adds padding around an object. Like layout.NewCustomPaddedLayout,
// but it never gives the object a negative size: while the window is being laid out,
// the space can be smaller than the padding, and Fyne's renderer crashes on negative sizes.
// A negative padding on top is allowed: the object shifts up.
type padLayout struct{ top, bottom, left, right float32 }

func (p padLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for _, o := range objects {
		if o.Visible() {
			s = s.Max(o.MinSize())
		}
	}
	return s.AddWidthHeight(p.left+p.right, max(p.top+p.bottom, 0))
}

func (p padLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	pos := fyne.NewPos(p.left, p.top)
	inner := fyne.NewSize(max(size.Width-p.left-p.right, 0), max(size.Height-p.top-p.bottom, 0))
	for _, o := range objects {
		o.Move(pos)
		o.Resize(inner)
	}
}

// edgeLayout is like Fyne's Border (top, bottom, left, right and centre), but
// without gaps between the parts. The standard Border puts the theme's padding
// (6 px) between them, and where different colours sit side by side (the grey sidebar,
// the grey bottom strip), the window background shows through the gap.
type edgeLayout struct {
	top, bottom, left, right fyne.CanvasObject
}

func (l edgeLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var center fyne.Size
	for _, o := range objects {
		if o.Visible() && o != l.top && o != l.bottom && o != l.left && o != l.right {
			center = center.Max(o.MinSize())
		}
	}
	size := func(o fyne.CanvasObject) fyne.Size {
		if o == nil || !o.Visible() {
			return fyne.Size{}
		}
		return o.MinSize()
	}
	left, right, top, bottom := size(l.left), size(l.right), size(l.top), size(l.bottom)

	width := max(left.Width+center.Width+right.Width, top.Width, bottom.Width)
	height := top.Height + max(left.Height, center.Height, right.Height) + bottom.Height
	return fyne.NewSize(width, height)
}

func (l edgeLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var topH, bottomH, leftW, rightW float32

	if l.top != nil && l.top.Visible() {
		topH = l.top.MinSize().Height
		l.top.Move(fyne.NewPos(0, 0))
		l.top.Resize(fyne.NewSize(size.Width, topH))
	}
	if l.bottom != nil && l.bottom.Visible() {
		bottomH = l.bottom.MinSize().Height
		l.bottom.Move(fyne.NewPos(0, size.Height-bottomH))
		l.bottom.Resize(fyne.NewSize(size.Width, bottomH))
	}
	middleH := max(size.Height-topH-bottomH, 0)
	if l.left != nil && l.left.Visible() {
		leftW = l.left.MinSize().Width
		l.left.Move(fyne.NewPos(0, topH))
		l.left.Resize(fyne.NewSize(leftW, middleH))
	}
	if l.right != nil && l.right.Visible() {
		rightW = l.right.MinSize().Width
		l.right.Move(fyne.NewPos(size.Width-rightW, topH))
		l.right.Resize(fyne.NewSize(rightW, middleH))
	}

	for _, o := range objects {
		if o.Visible() && o != l.top && o != l.bottom && o != l.left && o != l.right {
			o.Move(fyne.NewPos(leftW, topH))
			o.Resize(fyne.NewSize(max(size.Width-leftW-rightW, 0), middleH))
		}
	}
}

// edges is an edgeLayout container: the window parts sit flush against each other.
// The order of arguments is the same as in container.NewBorder.
func edges(top, bottom, left, right fyne.CanvasObject, center ...fyne.CanvasObject) *fyne.Container {
	objects := append([]fyne.CanvasObject{}, center...)
	for _, o := range []fyne.CanvasObject{top, bottom, left, right} {
		if o != nil {
			objects = append(objects, o)
		}
	}
	return container.New(edgeLayout{top: top, bottom: bottom, left: left, right: right}, objects...)
}

// pad is an object with padding: top, bottom, left, right.
func pad(top, bottom, left, right float32, objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(padLayout{top, bottom, left, right}, objects...)
}

// link is a button-link in blue text, like "Select All" in Apple apps.
// A Hyperlink without an address just calls onTap.
func link(s string, onTap func()) *widget.Hyperlink {
	h := widget.NewHyperlink(s, nil)
	h.OnTapped = onTap
	return h
}

// withAlpha is the same colour with a different transparency (0 is transparent, 255 is solid).
func withAlpha(c color.Color, a uint8) color.Color {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = a
	return n
}

// roundedRect is a rounded rectangle of one colour.
func roundedRect(c color.Color, radius float32) *canvas.Rectangle {
	r := canvas.NewRectangle(c)
	r.CornerRadius = radius
	return r
}

// hairline is a thin dividing line across the full width.
func hairline() fyne.CanvasObject {
	line := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	line.SetMinSize(fyne.NewSize(1, 1))
	return line
}

// gap is an empty space of a given size (a spacer).
func gap(w, h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(w, h))
	return r
}

// badge is a section icon: a white drawing on a coloured rounded card,
// like in the System Settings sidebar.
func badge(svg []byte, tint color.Color, size float32) fyne.CanvasObject {
	bg := canvas.NewRectangle(tint)
	bg.CornerRadius = size * 0.24

	var icon fyne.CanvasObject = gap(0, 0)
	if len(svg) > 0 {
		// The resource name comes from the content: Fyne caches images by name,
		// and with the same name for every section they would all get the same icon.
		name := fmt.Sprintf("icon-%08x.svg", crc32.ChecksumIEEE(svg))
		img := canvas.NewImageFromResource(fyne.NewStaticResource(name, svg))
		img.FillMode = canvas.ImageFillContain
		icon = img
	}

	inset := size * 0.2 // the drawing's margin from the edges of the card
	// GridWrap makes every object exactly the given size.
	return container.NewGridWrap(fyne.NewSize(size, size),
		container.NewStack(bg, pad(inset, inset, inset, inset, icon)))
}

// symbol is a small icon of the needed colour (a standard Fyne icon,
// recoloured to the theme colour).
func symbol(res fyne.Resource, colorName fyne.ThemeColorName, size float32) fyne.CanvasObject {
	img := canvas.NewImageFromResource(theme.NewColoredResource(res, colorName))
	img.FillMode = canvas.ImageFillContain
	return container.NewGridWrap(fyne.NewSize(size, size), img)
}

// tappable is an area you can click, with a highlight on hover.
// It is needed for the sidebar rows: a plain button looks different.
//
// A custom widget in Fyne: we embed widget.BaseWidget, call ExtendBaseWidget
// and provide CreateRenderer, which says "how to draw". The methods Tapped / MouseIn / MouseOut
// make the widget clickable and sensitive to hovering.
type tappable struct {
	widget.BaseWidget
	content fyne.CanvasObject
	hover   *canvas.Rectangle
	onTap   func()
}

func newTappable(content fyne.CanvasObject, onTap func()) *tappable {
	hover := canvas.NewRectangle(color.Transparent)
	hover.CornerRadius = theme.Size(theme.SizeNameSelectionRadius)

	t := &tappable{content: content, hover: hover, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappable) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(t.hover, t.content))
}

// Tapped is a click.
func (t *tappable) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

// MouseIn / MouseMoved / MouseOut handle mouse hovering (the desktop.Hoverable interface).
func (t *tappable) MouseIn(*desktop.MouseEvent) {
	t.hover.FillColor = theme.Color(theme.ColorNameHover)
	t.hover.Refresh()
}

func (t *tappable) MouseMoved(*desktop.MouseEvent) {}

func (t *tappable) MouseOut() {
	t.hover.FillColor = color.Transparent
	t.hover.Refresh()
}

// segment is one part of the disk fill bar.
type segment struct {
	size  float64
	color color.Color
}

// barLayout lays out the parts of the bar across the width in proportion to their sizes.
// A custom Layout in Fyne is two methods: MinSize and Layout.
type barLayout struct {
	weights []float64
	gap     float32
}

func (l barLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(100, 14) }

func (l barLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var sum float64
	for _, w := range l.weights {
		sum += w
	}
	if sum == 0 {
		return
	}

	// Until the window is laid out, the width can be zero; a rectangle must not have
	// a negative width, because Fyne's renderer crashes on it.
	width := max(size.Width-l.gap*float32(len(objects)-1), 0)
	x := float32(0)
	for i, o := range objects {
		w := width * float32(l.weights[i]/sum)
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w + l.gap
	}
}

// storageBar is a bar like "Storage" in macOS: used space, what was found
// for cleaning (in the sections' colours) and free space. Only the outer edges are rounded.
func storageBar(parts []segment) fyne.CanvasObject {
	var objects []fyne.CanvasObject
	var weights []float64

	for _, p := range parts {
		if p.size <= 0 {
			continue
		}
		// Very small parts are still visible as at least a thin line.
		weights = append(weights, max(p.size, 0))
		objects = append(objects, canvas.NewRectangle(p.color))
	}
	if len(objects) == 0 {
		return gap(0, 14)
	}

	const r = 4
	first := objects[0].(*canvas.Rectangle)
	first.TopLeftCornerRadius, first.BottomLeftCornerRadius = r, r
	last := objects[len(objects)-1].(*canvas.Rectangle)
	last.TopRightCornerRadius, last.BottomRightCornerRadius = r, r

	return container.New(barLayout{weights: weights, gap: 2}, objects...)
}
