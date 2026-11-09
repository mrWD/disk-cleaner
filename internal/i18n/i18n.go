// Package i18n — переводы интерфейса.
//
// Почему не встроенный пакет Fyne (fyne.io/fyne/v2/lang): он берёт язык
// только из системы и не умеет переключать его из настроек приложения,
// а если перевода нет — показывает «запасной» текст из кода. Здесь язык
// можно выбрать в настройках, а при отсутствии перевода берётся английский.
//
// Файлы переводов — плоский JSON {"ключ": "текст"}, имя файла — язык:
// en.json, ru.json. В тексте можно подставлять значения: "Free: {{.Free}}".
package i18n

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/jeandeaual/go-locale"
)

// Fallback — язык, на котором есть все тексты. Если перевода на выбранный
// язык нет, берётся отсюда.
const Fallback = "en"

// Catalog — все загруженные тексты и выбранный язык.
type Catalog struct {
	// mu защищает поля ниже: язык могут поменять в настройках, пока
	// фоновая горутина сканирования читает тексты. RWMutex пускает много
	// читателей одновременно, но писателя — только одного.
	mu       sync.RWMutex
	messages map[string]map[string]string // язык → ключ → текст
	chosen   string                       // выбран в настройках; "" — как в системе
	system   []string                     // языки системы по порядку предпочтения
}

// New создаёт пустой каталог, язык системы берётся из настроек macOS.
func New() *Catalog {
	langs, err := locale.GetLocales() // например ["en-US", "en", "fr", ...]
	if err != nil {
		langs = nil
	}
	return NewWithSystem(langs)
}

// NewWithSystem — то же, но языки системы задаются явно. Нужно для тестов:
// результат не должен зависеть от настроек машины, где их запускают.
func NewWithSystem(system []string) *Catalog {
	return &Catalog{messages: map[string]map[string]string{}, system: system}
}

// Add загружает все файлы dir/*.json из fsys. fs.FS — «файловая система»
// вообще: настоящий диск или файлы, вшитые в программу через embed.
// Ключи из разных файлов одного языка складываются вместе.
func (c *Catalog) Add(fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || path.Ext(name) != ".json" {
			continue
		}

		data, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return err
		}

		var texts map[string]string
		if err := json.Unmarshal(data, &texts); err != nil {
			return fmt.Errorf("%s/%s: %w", dir, name, err)
		}

		lang := strings.TrimSuffix(name, ".json")
		if c.messages[lang] == nil {
			c.messages[lang] = map[string]string{}
		}
		for k, v := range texts {
			c.messages[lang][k] = v
		}
	}

	return nil
}

// SetLanguage выбирает язык. "" — как в системе.
func (c *Catalog) SetLanguage(lang string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chosen = lang
}

// Chosen — что выбрано в настройках ("" — как в системе).
func (c *Catalog) Chosen() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.chosen
}

// Language — язык, на котором сейчас показываются тексты.
func (c *Catalog) Language() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.language()
}

// language — то же без блокировки, для вызова изнутри уже заблокированных методов.
func (c *Catalog) language() string {
	if _, ok := c.messages[c.chosen]; ok && c.chosen != "" {
		return c.chosen
	}

	// Первый язык системы, для которого есть тексты. "en-US" → "en".
	for _, l := range c.system {
		parts := strings.FieldsFunc(l, func(r rune) bool { return r == '-' || r == '_' })
		if len(parts) == 0 {
			continue // пустая строка вместо языка — пропускаем
		}
		base := strings.ToLower(parts[0])
		if _, ok := c.messages[base]; ok {
			return base
		}
	}

	return Fallback
}

// Languages — языки, для которых есть тексты, по алфавиту.
func (c *Catalog) Languages() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []string
	for l := range c.messages {
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

// Has — есть ли текст для ключа на этом языке. Нужен тестам,
// которые проверяют, что все ключи переведены.
func (c *Catalog) Has(lang, key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.messages[lang][key] // чтение из nil-map безопасно: вернёт "нет"
	return ok
}

// Keys — все ключи языка.
func (c *Catalog) Keys(lang string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []string
	for k := range c.messages[lang] {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// T — текст для ключа на текущем языке. Если перевода нет — английский,
// если нет и его — сам ключ (так пропущенный перевод сразу заметен).
// args — значения для подстановки: T("free", map[string]any{"Free": "10 GB"}).
func (c *Catalog) T(key string, args ...map[string]any) string {
	c.mu.RLock()
	text, ok := c.messages[c.language()][key]
	if !ok {
		text, ok = c.messages[Fallback][key]
	}
	c.mu.RUnlock()

	if !ok {
		return key
	}
	if len(args) == 0 || !strings.Contains(text, "{{") {
		return text
	}

	// text/template — шаблоны Go: {{.Free}} заменится на args[0]["Free"].
	tmpl, err := template.New(key).Option("missingkey=zero").Parse(text)
	if err != nil {
		return text
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, args[0]); err != nil {
		return text
	}
	return buf.String()
}
