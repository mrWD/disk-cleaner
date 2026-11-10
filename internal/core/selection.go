package core

import (
	"fmt"
	"maps"
	"slices"
)

// BlockedError is the error "this item can't be selected". It holds which
// item and why. In Go an error is any type with an Error() string method:
// the error type is an interface with exactly one such method.
type BlockedError struct {
	Item   Item
	Reason Reason
}

// Error makes BlockedError an error. The text is for logs and the developer;
// the UI shows the user a translation looked up by Reason.Key.
func (e *BlockedError) Error() string {
	return fmt.Sprintf("cannot select %q: %s", e.Item.Name, e.Reason.Key)
}

// Selection is what the user has marked for deletion.
//
// This is an aggregate (a DDD term): the items field starts with a lowercase letter,
// so it is invisible outside the core package and nothing can be put into it directly.
// The selection can only be changed through the methods below, and they enforce the rules.
type Selection struct {
	items map[string]Item // the key is keyOf(item), the value is the item itself
}

// NewSelection creates an empty selection.
// A map has to be created explicitly: the zero value of a map is nil,
// and writing to a nil map crashes the program.
func NewSelection() *Selection {
	return &Selection{items: map[string]Item{}}
}

// keyOf is an item's key, unique across the whole app.
// An ID is unique only within a category (a Docker image and a container
// can have the same ID), so we add the category.
// Lowercase name: a helper function for this package only.
func keyOf(it Item) string {
	return it.CategoryID + "/" + it.ID
}

// Add marks an item. If it can't be selected, it returns a *BlockedError
// and changes nothing. Adding the same item again changes nothing either.
//
// (s *Selection) is a pointer receiver: the method changes the Selection
// itself, not a copy of it.
func (s *Selection) Add(it Item) error {
	// if with a short declaration: ok and why are visible only inside this if.
	if ok, why := it.CanSelect(); !ok {
		return &BlockedError{Item: it, Reason: *why} // *why is the value the pointer points to
	}

	s.items[keyOf(it)] = it

	return nil
}

// Remove unmarks an item. If it wasn't marked, nothing happens.
func (s *Selection) Remove(it Item) {
	delete(s.items, keyOf(it)) // built-in function for deleting from a map
}

// Contains tells whether the item is marked.
func (s *Selection) Contains(it Item) bool {
	// Reading from a map returns two values: the value itself and "was it found".
	// We don't need the value, hence the _.
	_, found := s.items[keyOf(it)]
	return found
}

// Len is how many items are marked.
func (s *Selection) Len() int {
	return len(s.items)
}

// Total is how much space will be freed.
func (s *Selection) Total() Bytes {
	var total Bytes // the zero value is 0

	for _, it := range s.items {
		total += it.Size
	}

	return total
}

// Clear removes all marks.
func (s *Selection) Clear() {
	clear(s.items) // built-in function: removes all entries from the map
}

// Items returns all marked items in a stable order.
// Go deliberately makes map iteration order random, so we sort the keys:
// otherwise the deletion plan would look different every time it is opened.
func (s *Selection) Items() []Item {
	keys := slices.Sorted(maps.Keys(s.items)) // map keys -> sorted slice

	// make creates an empty slice with the needed capacity, so that append
	// doesn't reallocate memory on every step.
	out := make([]Item, 0, len(keys))
	for _, k := range keys {
		out = append(out, s.items[k])
	}

	return out
}

// ItemsFor returns the marked items that belong to provider p:
// those whose category is in p.Categories(). This way the core doesn't
// have to guess the provider from the start of the CategoryID string.
func (s *Selection) ItemsFor(p Provider) []Item {
	// map[string]bool used as a set: "does the provider have this category".
	own := map[string]bool{}
	for _, c := range p.Categories() {
		own[c.ID] = true
	}

	var out []Item
	for _, it := range s.Items() {
		if own[it.CategoryID] {
			out = append(out, it)
		}
	}

	return out
}
