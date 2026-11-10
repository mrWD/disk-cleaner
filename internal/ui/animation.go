package ui

import (
	"image/color"
	"math"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"disk-cleaner/internal/core"
)

// What moves while a section scans:
//
//   - a spinner like the macOS one, in the section's sidebar row, instead of
//     the word "Scanning…";
//   - in the section itself, while nothing is found yet: its icon with a ring
//     of dots running round it;
//   - as soon as something is measured: live progress, a running total of
//     what's been measured so far and the last few folders looked at. The
//     scan reports these through its context (core.Reporter, see scan.go).
//
// Each scanning section gets these once and keeps the same objects until
// the scan ends. That matters: the window is rebuilt every time any section
// finishes, and an animation created anew on each rebuild starts over from
// the beginning, which looked like the scan animation jerking.
//
// Fyne can't rotate an image, so the spinner is twelve fixed spokes whose
// brightness steps round the circle, and the ring is eight fixed dots done
// the same way. The real macOS spinner works like that too.

// stopAnimations stops a section's animations and forgets its spinner and
// skeleton. Called when its scan ends, and before the window is rebuilt for
// a new theme or language (the next build makes new ones in the new colours).
func (st *providerState) stopAnimations() {
	for _, a := range st.anims {
		a.Stop()
	}
	st.anims = nil
	st.spinner = nil
	st.view = nil
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

// scanProgress collects what a scan reports (it is the scan's core.Reporter).
// Reports come from the scan's goroutines, the window reads them from the
// UI thread, so a mutex guards the fields.
type scanProgress struct {
	mu       sync.Mutex
	measured core.Bytes
	recent   []string // the last folders looked at, newest last
}

const recentPaths = 4 // how many folders the live progress lists

func (p *scanProgress) Looking(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recent = append(p.recent, path)
	if len(p.recent) > recentPaths {
		p.recent = p.recent[len(p.recent)-recentPaths:]
	}
}

func (p *scanProgress) Measured(_ string, size core.Bytes) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.measured += size
}

// snapshot — the current total and the recent folders, newest first.
func (p *scanProgress) snapshot() (core.Bytes, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.recent))
	for i, path := range p.recent {
		out[len(p.recent)-1-i] = path
	}
	return p.measured, out
}

// scanView — the parts of what a scanning section shows, kept so the
// animation (and tests) can switch between them and update the text.
type scanView struct {
	root   fyne.CanvasObject // what goes into the section
	orbit  fyne.CanvasObject // nothing found yet: the icon with the ring
	live   fyne.CanvasObject // something measured: the total and the folders
	total  *canvas.Text
	paths  []*canvas.Text
	nowAt  *canvas.Text // under the ring: the folder being looked at
	update func()       // refresh from the progress; the animation calls it
}

// scanning — the section's scan view, made on first use.
func (u *App) scanning(st *providerState) fyne.CanvasObject {
	if st.view != nil {
		return st.view.root
	}

	icon, tint := u.look(st.provider)
	ring, step := newOrbit(icon, tint)

	v := &scanView{
		total: text("", sizeTitle, theme.ColorNameForeground, true),
		nowAt: text("", sizeCaption, colorSecondary, false),
	}
	v.orbit = container.NewVBox(
		container.NewCenter(ring),
		gap(0, 4),
		container.NewCenter(text(u.t("scan.nothing_yet"), theme.SizeNameText, colorSecondary, false)),
		container.NewCenter(v.nowAt),
	)

	live := container.NewVBox(
		container.NewCenter(v.total),
		container.NewCenter(text(u.t("scan.measured"), theme.SizeNameText, colorSecondary, false)),
		gap(0, 10),
	)
	for range recentPaths {
		p := text("", sizeCaption, colorSecondary, false)
		p.TextStyle = fyne.TextStyle{Monospace: true}
		v.paths = append(v.paths, p)
		live.Add(container.NewCenter(p))
	}
	v.live = live
	v.live.Hide()

	// Center keeps it in the middle of whatever space the section has.
	stack := container.NewStack(v.orbit, v.live)
	v.root = container.NewCenter(stack)

	// The paths fade from the newest (full grey) to the oldest (faint).
	grey := theme.Color(colorSecondary)
	v.update = func() {
		measured, recent := st.progress.snapshot()
		if measured == 0 {
			v.nowAt.Text = ""
			if len(recent) > 0 {
				v.nowAt.Text = shortPath(recent[0])
			}
			v.nowAt.Refresh()
			v.root.Refresh() // a longer path is wider: centre it again
			return
		}

		if !v.live.Visible() { // the first thing was measured: switch views
			v.orbit.Hide()
			v.live.Show()
		}
		v.total.Text = measured.String()
		v.total.Refresh()
		for i, p := range v.paths {
			p.Text = ""
			if i < len(recent) {
				p.Text = shortPath(recent[i])
			}
			p.Color = withAlpha(grey, uint8(255-i*55))
			p.Refresh()
		}
		v.root.Refresh() // new text has a new width and height: centre it again
	}

	// One clock for the ring and the text: eight steps a second is smooth for
	// the dots and calm for the numbers, and nothing redraws between steps.
	last := -1
	anim := fyne.NewAnimation(time.Second, func(p float32) {
		s := int(p*8) % 8
		if s == last {
			return
		}
		last = s
		step(s)
		v.update()
	})
	anim.RepeatCount = fyne.AnimationRepeatForever
	anim.Curve = fyne.AnimationLinear

	st.view = v
	st.anims = append(st.anims, anim)
	anim.Start()
	return v.root
}

// newOrbit draws the section icon with a ring of eight dots round it, in the
// section's colour. step(i) makes dot i the brightest, with a fading tail
// behind it; calling it with 0…7 in turn makes the dots run round.
func newOrbit(icon []byte, tint color.Color) (fyne.CanvasObject, func(int)) {
	const (
		n      = 8
		box    = float32(88)
		radius = float32(38)
		dot    = float32(7)
	)

	dots := make([]*canvas.Circle, n)
	objs := make([]fyne.CanvasObject, n)
	for i := range dots {
		angle := float64(i) * 2 * math.Pi / n
		x := box/2 + radius*float32(math.Sin(angle)) - dot/2
		y := box/2 - radius*float32(math.Cos(angle)) - dot/2
		c := canvas.NewCircle(tint)
		c.Move(fyne.NewPos(x, y))
		c.Resize(fyne.NewSize(dot, dot))
		dots[i], objs[i] = c, c
	}

	step := func(s int) {
		for i, d := range dots {
			behind := (s - i + n) % n // 0 = the bright one
			d.FillColor = withAlpha(tint, uint8(255-behind*28))
			d.Refresh()
		}
	}
	step(0)

	return container.NewStack(gap(box, box), container.NewWithoutLayout(objs...), container.NewCenter(badge(icon, tint, 44))), step
}

// shortPath shows a path with ~ for the home folder, cut from the front if
// it's long: the end of a path says more than its beginning.
func shortPath(p string) string {
	const limit = 70
	p = tildePath(p)
	if r := []rune(p); len(r) > limit {
		return "…" + string(r[len(r)-limit+1:])
	}
	return p
}
