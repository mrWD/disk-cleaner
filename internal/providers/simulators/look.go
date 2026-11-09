package simulators

import _ "embed" // _ — пакет нужен только ради //go:embed ниже

// Значок для списка слева: белый SVG, рисуется поверх цветной плашки.
// //go:embed кладёт содержимое файла в переменную при сборке.
//
//go:embed icon.svg
var icon []byte

// Look — значок и цвет плашки (индиго из системной палитры macOS).
// Реализует необязательный интерфейс core.Look.
func (p *Provider) Look() ([]byte, string) { return icon, "#5856D6" }
