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

// Кирпичики оформления в духе macOS: плашки, «таблетки», значки разделов,
// тонкие разделители. Из них собираются все экраны.
//
// canvas.* — это «рисование»: прямоугольник, текст, картинка. Они ничего
// не умеют, кроме как выглядеть. widget.* — интерактивные элементы.

// card — «сгруппированная» плашка как в Системных настройках:
// белый (в тёмном виде — серый) скруглённый прямоугольник с тонкой рамкой.
func card(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(colorCard))
	bg.CornerRadius = 10
	bg.StrokeColor = theme.Color(colorCardStroke)
	bg.StrokeWidth = 1

	// Stack кладёт объекты друг на друга: фон снизу, содержимое сверху.
	return container.NewStack(bg, pad(10, 10, 14, 14, content))
}

// text — надпись нужного размера, цвета и жирности. В отличие от Label
// она не переносится по словам — для коротких заголовков и цифр.
func text(s string, size fyne.ThemeSizeName, colorName fyne.ThemeColorName, bold bool) *canvas.Text {
	t := canvas.NewText(s, theme.Color(colorName))
	t.TextSize = theme.Size(size)
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}

// secondary — серый текст с переносом по словам (или с обрезкой «…»,
// если wrap = false). RichText умеет и свой цвет, и перенос.
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

// colored — текст заданного цвета с переносом (для предупреждений).
func colored(s string, colorName fyne.ThemeColorName) *widget.RichText {
	rt := widget.NewRichText(&widget.TextSegment{
		Text:  s,
		Style: widget.RichTextStyle{ColorName: colorName, SizeName: sizeCaption, Inline: true},
	})
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// pill — «таблетка» как у статусов в macOS: цветной текст на бледном
// фоне того же цвета, со скруглёнными краями.
func pill(s string, colorName fyne.ThemeColorName) fyne.CanvasObject {
	c := theme.Color(colorName)

	label := text(s, theme.SizeNameCaptionText, colorName, true)
	bg := canvas.NewRectangle(withAlpha(c, 0x2E)) // ~18% непрозрачности
	bg.CornerRadius = 8

	return container.NewCenter( // Center — не растягивать таблетку по высоте строки
		container.NewStack(bg, pad(2, 2, 8, 8, label)),
	)
}

// padLayout — отступы вокруг объекта. Как layout.NewCustomPaddedLayout,
// но никогда не даёт объекту отрицательный размер: пока окно раскладывается,
// место бывает меньше отступов, а рисовальщик Fyne на отрицательных размерах
// падает. Отрицательный отступ сверху допустим — объект сдвигается вверх.
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

// edgeLayout — как Border из Fyne (верх, низ, лево, право и центр), но
// без зазоров между частями. Стандартный Border вставляет между ними
// отступ темы (6 px), и там, где рядом разные цвета (серая боковая панель,
// серая нижняя полоса), сквозь щель проглядывает фон окна.
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

// edges — контейнер edgeLayout: части окна вплотную друг к другу.
// Порядок аргументов — как у container.NewBorder.
func edges(top, bottom, left, right fyne.CanvasObject, center ...fyne.CanvasObject) *fyne.Container {
	objects := append([]fyne.CanvasObject{}, center...)
	for _, o := range []fyne.CanvasObject{top, bottom, left, right} {
		if o != nil {
			objects = append(objects, o)
		}
	}
	return container.New(edgeLayout{top: top, bottom: bottom, left: left, right: right}, objects...)
}

// pad — объект с отступами: сверху, снизу, слева, справа.
func pad(top, bottom, left, right float32, objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(padLayout{top, bottom, left, right}, objects...)
}

// link — кнопка-ссылка синим текстом, как «Select All» в приложениях Apple.
// Hyperlink без адреса просто вызывает onTap.
func link(s string, onTap func()) *widget.Hyperlink {
	h := widget.NewHyperlink(s, nil)
	h.OnTapped = onTap
	return h
}

// withAlpha — тот же цвет с другой прозрачностью (0 — прозрачный, 255 — сплошной).
func withAlpha(c color.Color, a uint8) color.Color {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = a
	return n
}

// roundedRect — скруглённый прямоугольник одного цвета.
func roundedRect(c color.Color, radius float32) *canvas.Rectangle {
	r := canvas.NewRectangle(c)
	r.CornerRadius = radius
	return r
}

// hairline — тонкая линия-разделитель на всю ширину.
func hairline() fyne.CanvasObject {
	line := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	line.SetMinSize(fyne.NewSize(1, 1))
	return line
}

// gap — пустое место заданного размера (отступ).
func gap(w, h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(w, h))
	return r
}

// badge — значок раздела: белый рисунок на цветной скруглённой плашке,
// как в боковой панели Системных настроек.
func badge(svg []byte, tint color.Color, size float32) fyne.CanvasObject {
	bg := canvas.NewRectangle(tint)
	bg.CornerRadius = size * 0.24

	var icon fyne.CanvasObject = gap(0, 0)
	if len(svg) > 0 {
		// Имя ресурса — по содержимому: Fyne кэширует картинки по имени,
		// и с одинаковым именем у всех разделов был бы один и тот же значок.
		name := fmt.Sprintf("icon-%08x.svg", crc32.ChecksumIEEE(svg))
		img := canvas.NewImageFromResource(fyne.NewStaticResource(name, svg))
		img.FillMode = canvas.ImageFillContain
		icon = img
	}

	inset := size * 0.2 // отступ рисунка от краёв плашки
	// GridWrap делает каждый объект ровно заданного размера.
	return container.NewGridWrap(fyne.NewSize(size, size),
		container.NewStack(bg, pad(inset, inset, inset, inset, icon)))
}

// symbol — маленький значок нужного цвета (стандартный значок Fyne,
// перекрашенный в цвет темы).
func symbol(res fyne.Resource, colorName fyne.ThemeColorName, size float32) fyne.CanvasObject {
	img := canvas.NewImageFromResource(theme.NewColoredResource(res, colorName))
	img.FillMode = canvas.ImageFillContain
	return container.NewGridWrap(fyne.NewSize(size, size), img)
}

// tappable — область, по которой можно кликнуть, с подсветкой при наведении.
// Нужна для строк боковой панели: обычная кнопка выглядит не так.
//
// Свой виджет в Fyne: встраиваем widget.BaseWidget, вызываем ExtendBaseWidget
// и даём CreateRenderer — «как рисовать». Методы Tapped / MouseIn / MouseOut
// делают виджет кликабельным и чувствительным к наведению.
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

// Tapped — клик.
func (t *tappable) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

// MouseIn / MouseMoved / MouseOut — наведение мыши (интерфейс desktop.Hoverable).
func (t *tappable) MouseIn(*desktop.MouseEvent) {
	t.hover.FillColor = theme.Color(theme.ColorNameHover)
	t.hover.Refresh()
}

func (t *tappable) MouseMoved(*desktop.MouseEvent) {}

func (t *tappable) MouseOut() {
	t.hover.FillColor = color.Transparent
	t.hover.Refresh()
}

// segment — одна часть полосы заполнения диска.
type segment struct {
	size  float64
	color color.Color
}

// barLayout раскладывает части полосы по ширине пропорционально размерам.
// Свой Layout в Fyne — это два метода: MinSize и Layout.
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

	// Пока окно не разложено, ширина бывает нулевой; отрицательной
	// ширины у прямоугольника быть не должно — рисовальщик Fyne на ней падает.
	width := max(size.Width-l.gap*float32(len(objects)-1), 0)
	x := float32(0)
	for i, o := range objects {
		w := width * float32(l.weights[i]/sum)
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w + l.gap
	}
}

// storageBar — полоса как в «Хранилище» macOS: занятое, найденное
// к очистке (цветом разделов) и свободное. Скруглены только внешние края.
func storageBar(parts []segment) fyne.CanvasObject {
	var objects []fyne.CanvasObject
	var weights []float64

	for _, p := range parts {
		if p.size <= 0 {
			continue
		}
		// Очень маленькие части всё равно видны хотя бы полоской.
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
