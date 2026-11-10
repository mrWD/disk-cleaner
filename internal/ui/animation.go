package ui

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// What moves while a section scans:
//
//   - a spinner like the macOS one, in the section's sidebar row, instead of
//     the word "Scanning…";
//   - skeleton cards in the section itself: grey placeholders in the shape
//     of the result, pulsing in a wave.
//
// Each scanning section gets its spinner and skeleton once and keeps the
// same objects until the scan ends. That matters: the window is rebuilt
// every time any section finishes, and an animation created anew on each
// rebuild starts over from the beginning, which looked like the scan
// animation jerking.
//
// Fyne can't rotate an image, so the spinner is twelve fixed spokes whose
// brightness steps round the circle. The real macOS spinner works the same way.

// stopAnimations stops a section's animations and forgets its spinner and
// skeleton. Called when its scan ends, and before the window is rebuilt for
// a new theme or language (the next build makes new ones in the new colours).
func (st *providerState) stopAnimations() {
	for _, a := range st.anims {
		a.Stop()
	}
	st.anims = nil
	st.spinner = nil
	st.skeleton = nil
}

// spinner — the section's sidebar spinner, made on first use.
func (u *App) spinner(st *providerState) fyne.CanvasObject {
	if st.spinner == nil {
		obj, anim := newSpinner(14, theme.Color(colorSecondary))
		st.spinner, st.anims = obj, append(st.anims, anim)
		anim.Start()
	}
	return st.spinner
}

// skeleton — the section's skeleton cards, made on first use.
func (u *App) skeleton(st *providerState) fyne.CanvasObject {
	if st.skeleton == nil {
		obj, anim := newSkeleton(theme.Color(colorTrack))
		st.skeleton, st.anims = obj, append(st.anims, anim)
		anim.Start()
	}
	return st.skeleton
}

// newSpinner draws twelve spokes in a size × size square. The brightest
// spoke moves one step at a time, twelve steps a second, and the ones behind
// it fade, like the macOS activity indicator.
func newSpinner(size float32, c color.Color) (fyne.CanvasObject, *fyne.Animation) {
	const spokes = 12
	r, g, b, _ := c.RGBA() // 16-bit channels
	base := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}

	lines := make([]*canvas.Line, spokes)
	objs := make([]fyne.CanvasObject, spokes)
	mid := size / 2
	for i := range lines {
		angle := float64(i) * 2 * math.Pi / spokes
		sin, cos := float32(math.Sin(angle)), float32(math.Cos(angle))
		l := canvas.NewLine(base)
		l.StrokeWidth = size / 9
		// From a little off the centre to the edge, pointing outwards.
		l.Position1 = fyne.NewPos(mid+sin*size*0.22, mid-cos*size*0.22)
		l.Position2 = fyne.NewPos(mid+sin*size*0.46, mid-cos*size*0.46)
		lines[i], objs[i] = l, l
	}

	last := -1
	anim := fyne.NewAnimation(time.Second, func(p float32) {
		step := int(p*spokes) % spokes
		if step == last {
			return // only redraw when the bright spoke moves
		}
		last = step
		for i, l := range lines {
			behind := (step - i + spokes) % spokes // 0 = the bright one
			alpha := 1 - float32(behind)/spokes*0.85
			c := base
			c.A = uint8(alpha * 255)
			l.StrokeColor = c
			l.Refresh()
		}
	})
	anim.RepeatCount = fyne.AnimationRepeatForever
	anim.Curve = fyne.AnimationLinear

	// gap gives the square its size; the spokes keep their own positions
	// inside it, because NewWithoutLayout doesn't move anything.
	return container.NewStack(gap(size, size), container.NewWithoutLayout(objs...)), anim
}

// newSkeleton draws three placeholder cards shaped like category cards: a
// title, a size and two item rows each. Every bar pulses between faint and
// solid, a little later than the one before, so a soft wave runs down the cards.
func newSkeleton(c color.Color) (fyne.CanvasObject, *fyne.Animation) {
	var bars []*canvas.Rectangle
	bar := func(w, h float32) fyne.CanvasObject {
		r := canvas.NewRectangle(c)
		r.CornerRadius = h / 2
		r.SetMinSize(fyne.NewSize(w, h))
		bars = append(bars, r)
		return container.NewCenter(r) // keeps the bar at its own height in a taller row
	}
	row := func(left, right float32, h float32) fyne.CanvasObject {
		return container.NewBorder(nil, nil, bar(left, h), bar(right, h))
	}

	// Different widths per card, so it reads as content and not a pattern.
	widths := [][4]float32{{180, 70, 240, 60}, {140, 60, 300, 50}, {210, 80, 200, 70}}
	var cards []fyne.CanvasObject
	for _, w := range widths {
		cards = append(cards, pad(0, 8, 0, 0, card(container.NewVBox(
			pad(4, 4, 4, 4, row(w[0], w[1], 12)),
			hairline(),
			pad(6, 2, 4, 4, row(w[2], w[3], 9)),
			pad(6, 4, 4, 4, row(w[2]*0.7, w[3]*0.8, 9)),
		))))
	}

	r, g, b, _ := c.RGBA()
	base := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
	anim := fyne.NewAnimation(1600*time.Millisecond, func(p float32) {
		for i, rect := range bars {
			// A sine wave between 45% and 100% opacity, each bar a bit behind the last.
			wave := math.Sin(2 * math.Pi * (float64(p) - float64(i)*0.06))
			c := base
			c.A = uint8((0.725 + 0.275*wave) * 255)
			rect.FillColor = c
			rect.Refresh()
		}
	})
	anim.RepeatCount = fyne.AnimationRepeatForever
	anim.Curve = fyne.AnimationLinear

	return container.NewVBox(cards...), anim
}
