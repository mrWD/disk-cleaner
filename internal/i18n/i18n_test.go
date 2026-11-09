package i18n

import (
	"testing"
	"testing/fstest"
)

// testFS — «файловая система» в памяти: fstest.MapFS ведёт себя как папка
// с файлами, но ничего не пишет на диск.
var testFS = fstest.MapFS{
	"tr/en.json": {Data: []byte(`{"hello": "Hello", "free": "Free: {{.Size}}", "only_en": "English only"}`)},
	"tr/ru.json": {Data: []byte(`{"hello": "Привет", "free": "Свободно: {{.Size}}"}`)},
}

// catalog — каталог с тестовыми текстами и заданными языками системы.
func catalog(t *testing.T, system ...string) *Catalog {
	t.Helper()
	c := NewWithSystem(system)
	if err := c.Add(testFS, "tr"); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCatalog(t *testing.T) {
	t.Run("system language is used when nothing is chosen", func(t *testing.T) {
		c := catalog(t, "ru-RU", "en-US")
		if got := c.T("hello"); got != "Привет" {
			t.Errorf("T(hello) = %q, want Привет", got)
		}
	})

	t.Run("unknown system language falls back to English", func(t *testing.T) {
		c := catalog(t, "ja-JP")
		if got := c.T("hello"); got != "Hello" {
			t.Errorf("T(hello) = %q, want Hello", got)
		}
	})

	t.Run("language chosen in settings wins over the system", func(t *testing.T) {
		c := catalog(t, "en-US")
		c.SetLanguage("ru")
		if got := c.T("hello"); got != "Привет" {
			t.Errorf("T(hello) = %q, want Привет", got)
		}
	})

	t.Run("missing translation falls back to English", func(t *testing.T) {
		c := catalog(t, "ru")
		if got := c.T("only_en"); got != "English only" {
			t.Errorf("T(only_en) = %q", got)
		}
	})

	t.Run("missing key shows the key itself", func(t *testing.T) {
		c := catalog(t, "en")
		if got := c.T("nope.key"); got != "nope.key" {
			t.Errorf("T(nope.key) = %q", got)
		}
	})

	t.Run("values are substituted into the text", func(t *testing.T) {
		c := catalog(t, "ru")
		got := c.T("free", map[string]any{"Size": "113.6 GB"})
		if got != "Свободно: 113.6 GB" {
			t.Errorf("T(free) = %q", got)
		}
	})

	t.Run("broken JSON is reported", func(t *testing.T) {
		bad := fstest.MapFS{"tr/en.json": {Data: []byte(`{"a": `)}}
		if err := NewWithSystem(nil).Add(bad, "tr"); err == nil {
			t.Error("want an error for broken JSON")
		}
	})
}
