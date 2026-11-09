package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// Выбор оформления в настройках — три плитки с картинками, как в
// Системных настройках macOS (Оформление): «Светлое», «Тёмное», «Авто».
// На каждой плитке — маленькое окно на фоне рабочего стола в нужных цветах;
// у «Авто» левая половина светлая, правая — тёмная.

// Размер картинки на плитке.
const (
	previewW = 104
	previewH = 66
)

// Цвет «рабочего стола» на картинке: светлый и тёмный.
var (
	wallpaperLight = hex("#A9C1E3")
	wallpaperDark  = hex("#22304D")
)

// appearancePicker — три плитки и то, что сейчас выбрано.
type appearancePicker struct {
	u        *App
	selected string
	box      *fyne.Container // сюда кладутся плитки; перестраивается при выборе
}

// newAppearancePicker — плитки с уже выбранным current.
func (u *App) newAppearancePicker(current string) *appearancePicker {
	p := &appearancePicker{u: u, selected: current, box: container.NewHBox()}
	p.refill()
	return p
}

// pick — выбрать вариант (клик по плитке).
func (p *appearancePicker) pick(mode string) {
	p.selected = mode
	p.refill()
}

// refill перестраивает плитки, чтобы рамка была у выбранной.
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
		mode := o.mode // своя копия для замыкания ниже
		selected := mode == p.selected

		// Рамка выбранной плитки — синяя, у остальных — прозрачная.
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

// preview рисует маленькое окно на фоне рабочего стола. left и right —
// цвета левой и правой половины (у «Авто» они разные).
//
// container.NewWithoutLayout — контейнер без раскладки: каждый
// прямоугольник ставим руками через Move (где) и Resize (какого размера).
func preview(left, right palette, wallL, wallR color.Color) fyne.CanvasObject {
	const (
		w, h  = float32(previewW), float32(previewH)
		half  = w / 2
		winX  = 14 // окно: отступ слева
		winY  = 12 // и сверху; снизу окно уходит за край, как в macOS
		winR  = w - 10
		sideW = 22 // боковая панель окна
	)

	var objs []fyne.CanvasObject
	place := func(c color.Color, x, y, width, height float32) *canvas.Rectangle {
		r := canvas.NewRectangle(c)
		r.Move(fyne.NewPos(x, y))
		r.Resize(fyne.NewSize(width, height))
		objs = append(objs, r)
		return r
	}

	// Рабочий стол: две половины, скруглены только внешние углы.
	l := place(wallL, 0, 0, half, h)
	l.TopLeftCornerRadius, l.BottomLeftCornerRadius = 7, 7
	r := place(wallR, half, 0, half, h)
	r.TopRightCornerRadius, r.BottomRightCornerRadius = 7, 7

	// Окно: левая часть в цветах left, правая — в цветах right.
	wl := place(left.background, winX, winY, half-winX, h-winY)
	wl.TopLeftCornerRadius = 4
	wr := place(right.background, half, winY, winR-half, h-winY)
	wr.TopRightCornerRadius = 4

	// Боковая панель окна (в левой половине) и синяя «выбранная строка».
	place(left.sidebar, winX, winY+3, sideW, h-winY-3)
	place(left.accent, winX+3, winY+8, sideW-6, 4)

	// Две «строки текста» в окне — по половинке в каждой части.
	textX := float32(winX + sideW + 6)
	place(left.secondary, textX, winY+9, half-textX, 3)
	place(right.secondary, half, winY+9, winR-half-10, 3)
	place(left.tertiary, textX, winY+17, half-textX, 3)
	place(right.tertiary, half, winY+17, winR-half-22, 3)

	return container.NewWithoutLayout(objs...)
}
