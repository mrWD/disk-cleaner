package core

import "testing"

// Тесты в Go — функции с именем TestXxx в файлах *_test.go.
// Запускаются командой go test и в собранную программу не попадают.
func TestBytesString(t *testing.T) {
	// Таблица случаев: имя поведения, вход, ожидаемый результат.
	// struct { ... } прямо на месте — анонимная структура, как объект в JS.
	cases := []struct {
		name string
		in   Bytes
		want string
	}{
		{"zero", 0, "0 B"},
		{"less than a kilobyte stays in bytes", 999, "999 B"},
		{"exactly one kilobyte", 1000, "1.0 kB"},
		{"megabytes with one decimal", 1_500_000, "1.5 MB"}, // _ в числах — для читаемости
		{"typical free space", 104_300_000_000, "104.3 GB"},
	}

	// range перебирает срез: _ — индекс (не нужен), c — очередной случай.
	for _, c := range cases {
		// t.Run — подтест со своим именем. Имена описывают поведение,
		// как сценарии в BDD, и видны в выводе go test -v.
		t.Run(c.name, func(t *testing.T) {
			got := c.in.String()

			if got != c.want {
				// %q печатает строку в кавычках — так видны лишние пробелы.
				t.Errorf("Bytes(%d).String() = %q, want %q", int64(c.in), got, c.want)
			}
		})
	}
}
