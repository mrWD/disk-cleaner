package core

// Risk is how safe it is to clean a category.
type Risk int

const (
	// RiskSafe means it gets recreated by itself and nothing is lost.
	// Example: DerivedData, the npm cache.
	RiskSafe Risk = iota

	// RiskCaution means some state is lost that you would have to restore
	// by hand or download for a long time. Example: simulators, Android emulators.
	RiskCaution
)

// Category is a group of items of one kind, with an explanation for the user.
//
// The texts live in the provider's translations under keys built from the ID:
//
//	<ID>.title — the heading:               "Derived Data"
//	<ID>.what  — what it is
//	<ID>.why   — why it's safe to delete
//	<ID>.after — what happens after deleting
type Category struct {
	ID   string // "xcode.derived_data": provider first, then the category
	Risk Risk
}

// The methods below build the translation keys, so that the string ".what"
// isn't repeated all over the code with a risk of typos.
func (c Category) TitleKey() string { return c.ID + ".title" }
func (c Category) WhatKey() string  { return c.ID + ".what" }
func (c Category) WhyKey() string   { return c.ID + ".why" }
func (c Category) AfterKey() string { return c.ID + ".after" }
