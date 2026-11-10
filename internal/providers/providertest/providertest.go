// Package providertest holds shared checks for the tests of all providers.
//
// A regular package (not _test.go) so that tests in different folders can
// import it. It does not end up in the program itself: only tests
// import it.
package providertest

import (
	"testing"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/i18n"
)

// CheckTranslations checks that a provider has all its texts
// in all of its languages:
//   - "<id>.name": the provider's name in the list on the left;
//   - four texts for each category: title, what, why, after;
//   - extraKeys: keys of protection and unavailability reasons that the
//     provider uses in code (for example "docker.not_running").
//
// Exactly this kind of test would catch a typo like "core.reasong.in_use".
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
		// And the other way round: the translation has no extra keys that English doesn't have.
		for _, k := range catalog.Keys(lang) {
			if !catalog.Has(i18n.Fallback, k) {
				t.Errorf("%s.json: key %q is not in en.json", lang, k)
			}
		}
	}
}

// CheckCategories checks that all category IDs start with the provider's
// ID and that items refer only to their own categories.
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
