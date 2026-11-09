// Package providertest — общие проверки для тестов всех провайдеров.
//
// Обычный (не _test.go) пакет, чтобы его могли импортировать тесты
// из разных папок. В саму программу он не попадает: его импортируют
// только тесты.
package providertest

import (
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/i18n"
)

// CheckTranslations проверяет, что у провайдера есть все тексты
// на всех его языках:
//   - "<id>.name" — имя провайдера в списке слева;
//   - четыре текста каждой категории: title, what, why, after;
//   - extraKeys — ключи причин защиты и недоступности, которые
//     провайдер использует в коде (например "docker.not_running").
//
// Именно такой тест поймал бы опечатку вроде "core.reasong.in_use".
func CheckTranslations(t *testing.T, p core.Provider, extraKeys ...string) {
	t.Helper()

	catalog := i18n.NewWithSystem(nil)
	if err := catalog.Add(p.Translations(), "translations"); err != nil {
		t.Fatalf("load translations: %v", err)
	}

	keys := []string{p.ID() + ".name"}
	for _, c := range p.Categories() {
		keys = append(keys, c.TitleKey(), c.WhatKey(), c.WhyKey(), c.AfterKey())
	}
	keys = append(keys, extraKeys...)

	langs := catalog.Languages()
	if len(langs) == 0 {
		t.Fatal("no translation files in translations/")
	}

	for _, lang := range langs {
		for _, k := range keys {
			if !catalog.Has(lang, k) {
				t.Errorf("%s.json: missing %q", lang, k)
			}
		}
		// И наоборот: в переводе нет лишних ключей, которых нет в английском.
		for _, k := range catalog.Keys(lang) {
			if !catalog.Has(i18n.Fallback, k) {
				t.Errorf("%s.json: key %q is not in en.json", lang, k)
			}
		}
	}
}

// CheckCategories проверяет, что все ID категорий начинаются с ID
// провайдера и что объекты ссылаются только на свои категории.
func CheckCategories(t *testing.T, p core.Provider, items []core.Item) {
	t.Helper()

	own := map[string]bool{}
	for _, c := range p.Categories() {
		if len(c.ID) <= len(p.ID())+1 || c.ID[:len(p.ID())+1] != p.ID()+"." {
			t.Errorf("category %q must start with %q", c.ID, p.ID()+".")
		}
		own[c.ID] = true
	}

	for _, it := range items {
		if !own[it.CategoryID] {
			t.Errorf("item %q has foreign category %q", it.Name, it.CategoryID)
		}
	}
}
