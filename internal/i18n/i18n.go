// Package i18n holds the interface translations.
//
// Why not Fyne's built-in package (fyne.io/fyne/v2/lang): it takes the language
// only from the system and can't switch it from the app's settings,
// and if a translation is missing it shows a "fallback" text from the code. Here the
// language can be chosen in settings, and when a translation is missing English is used.
//
// Translation files are flat JSON {"key": "text"}, and the file name is the language:
// en.json, ru.json. Values can be substituted into the text: "Free: {{.Free}}".
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

// Fallback is the language that has every text. If there is no translation
// into the chosen language, the text comes from here.
const Fallback = "en"

// Catalog holds all the loaded texts and the chosen language.
type Catalog struct {
	// mu protects the fields below: the language can be changed in settings while
	// a background scanning goroutine is reading texts. An RWMutex lets many
	// readers in at once, but only one writer.
	mu       sync.RWMutex
	messages map[string]map[string]string // language -> key -> text
	chosen   string                       // chosen in settings; "" means same as the system
	system   []string                     // system languages in order of preference
}

// New creates an empty catalog; the system language is taken from the macOS settings.
func New() *Catalog {
	langs, err := locale.GetLocales() // for example ["en-US", "en", "fr", ...]
	if err != nil {
		langs = nil
	}
	return NewWithSystem(langs)
}

// NewWithSystem is the same, but the system languages are given explicitly. It is for tests:
// the result must not depend on the settings of the machine they run on.
func NewWithSystem(system []string) *Catalog {
	return &Catalog{messages: map[string]map[string]string{}, system: system}
}

// Add loads all the dir/*.json files from fsys. fs.FS is a "file system"
// in general: a real disk, or files baked into the program through embed.
// Keys from different files of the same language are merged together.
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

// SetLanguage chooses the language. "" means same as the system.
func (c *Catalog) SetLanguage(lang string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chosen = lang
}

// Chosen is what is selected in settings ("" means same as the system).
func (c *Catalog) Chosen() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.chosen
}

// Language is the language the texts are shown in right now.
func (c *Catalog) Language() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.language()
}

// language is the same without locking, for calling from inside methods that already hold the lock.
func (c *Catalog) language() string {
	if _, ok := c.messages[c.chosen]; ok && c.chosen != "" {
		return c.chosen
	}

	// The first system language that has texts. "en-US" -> "en".
	for _, l := range c.system {
		parts := strings.FieldsFunc(l, func(r rune) bool { return r == '-' || r == '_' })
		if len(parts) == 0 {
			continue // an empty string instead of a language: skip it
		}
		base := strings.ToLower(parts[0])
		if _, ok := c.messages[base]; ok {
			return base
		}
	}

	return Fallback
}

// Languages returns the languages that have texts, in alphabetical order.
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

// Has tells whether there is a text for the key in this language. Tests need it
// to check that all the keys are translated.
func (c *Catalog) Has(lang, key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.messages[lang][key] // reading from a nil map is safe: it just returns "not found"
	return ok
}

// Keys returns all the keys of a language.
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

// T returns the text for the key in the current language. If there is no translation,
// English; if that is missing too, the key itself (so a missing translation is spotted at once).
// args are the values to substitute: T("free", map[string]any{"Free": "10 GB"}).
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

	// text/template is Go's templating: {{.Free}} is replaced with args[0]["Free"].
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
