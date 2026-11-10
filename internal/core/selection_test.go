package core

import (
	"errors"
	"testing"
)

// Items for the scenarios. Package-level vars are visible in all core tests.
var (
	derivedData = Item{ID: "~/Library/Developer/Xcode/DerivedData", CategoryID: "xcode.derived_data", Name: "DerivedData", Size: 1_500_000_000}
	npmCache    = Item{ID: "~/.npm/_cacache", CategoryID: "packagecaches.npm", Name: "npm cache", Size: 500_000_000}
	redis       = Item{ID: "a1b2c3", CategoryID: "docker.containers", Name: "redis:8", Size: 98_000_000, Status: StatusRunning}
)

// mustAdd adds an item and fails the scenario if that doesn't work.
// t.Helper() tells Go: on failure, point at the line of the test that
// called mustAdd, not at a line inside mustAdd.
func mustAdd(t *testing.T, s *Selection, it Item) {
	t.Helper()

	if err := s.Add(it); err != nil {
		t.Fatalf("Add(%q) failed: %v", it.Name, err)
	}
}

func TestSelection(t *testing.T) {
	t.Run("empty selection has nothing to clean", func(t *testing.T) {
		// Given: a new selection
		s := NewSelection()

		// Then: it has nothing in it
		if s.Len() != 0 || s.Total() != 0 {
			t.Errorf("Len() = %d, Total() = %s, want 0 and 0 B", s.Len(), s.Total())
		}
	})

	t.Run("selecting idle items sums their size", func(t *testing.T) {
		// Given: a new selection
		s := NewSelection()

		// When: we mark two idle items
		mustAdd(t, s, derivedData)
		mustAdd(t, s, npmCache)

		// Then: both are in the selection, the size is their sum
		if s.Len() != 2 {
			t.Errorf("Len() = %d, want 2", s.Len())
		}
		if s.Total() != 2_000_000_000 {
			t.Errorf("Total() = %s, want 2.0 GB", s.Total())
		}
	})

	t.Run("running item is refused with a reason", func(t *testing.T) {
		// Given: a new selection
		s := NewSelection()

		// When: we try to mark a running container
		err := s.Add(redis)

		// Then: the error is specifically a *BlockedError with the reason "running"…
		// errors.As checks the error's type and stores it in blocked.
		var blocked *BlockedError
		if !errors.As(err, &blocked) {
			t.Fatalf("Add(redis) error = %v, want *BlockedError", err)
		}
		if blocked.Reason.Key != ReasonRunning {
			t.Errorf("reason = %q, want %q", blocked.Reason.Key, ReasonRunning)
		}

		// …and nothing got into the selection
		if s.Contains(redis) || s.Len() != 0 {
			t.Errorf("redis got into the selection")
		}
	})

	t.Run("selecting the same item twice counts it once", func(t *testing.T) {
		// Given: DerivedData is already marked
		s := NewSelection()
		mustAdd(t, s, derivedData)

		// When: we mark it again (a double click on the checkbox)
		mustAdd(t, s, derivedData)

		// Then: the size didn't double
		if s.Len() != 1 || s.Total() != derivedData.Size {
			t.Errorf("Len() = %d, Total() = %s, want 1 and %s", s.Len(), s.Total(), derivedData.Size)
		}
	})

	t.Run("unselecting removes the item from the total", func(t *testing.T) {
		// Given: two items are marked
		s := NewSelection()
		mustAdd(t, s, derivedData)
		mustAdd(t, s, npmCache)

		// When: we unmark one of them
		s.Remove(derivedData)

		// Then: only the second one is left
		if s.Contains(derivedData) || s.Total() != npmCache.Size {
			t.Errorf("Total() = %s, want %s", s.Total(), npmCache.Size)
		}
	})

	t.Run("same ID in different categories are different items", func(t *testing.T) {
		// Given: a Docker image and a container with the same ID
		image := Item{ID: "abc", CategoryID: "docker.images", Name: "postgres:17", Size: 300}
		container := Item{ID: "abc", CategoryID: "docker.containers", Name: "old-postgres", Size: 200}
		s := NewSelection()

		// When: we mark both
		mustAdd(t, s, image)
		mustAdd(t, s, container)

		// Then: they are two different items
		if s.Len() != 2 || s.Total() != 500 {
			t.Errorf("Len() = %d, Total() = %s, want 2 and 500 B", s.Len(), s.Total())
		}
	})

	t.Run("items come back in a stable order", func(t *testing.T) {
		// Given: items are marked in one order
		s := NewSelection()
		mustAdd(t, s, npmCache)
		mustAdd(t, s, derivedData)

		// When: we ask for the list several times
		first := s.Items()
		second := s.Items()

		// Then: the order is the same (by category, then by ID)
		if len(first) != 2 || first[0].Name != "npm cache" || second[0].Name != first[0].Name {
			t.Errorf("Items() order = %v / %v", first, second)
		}
	})

	t.Run("clear removes everything", func(t *testing.T) {
		// Given: something is marked
		s := NewSelection()
		mustAdd(t, s, derivedData)

		// When: we clear all the marks
		s.Clear()

		// Then: the selection is empty
		if s.Len() != 0 {
			t.Errorf("Len() = %d after Clear, want 0", s.Len())
		}
	})
}
