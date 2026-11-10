package core

import "testing"

// Scenarios in BDD style: Given → When → Then.
// The name of each scenario is a phrase about behaviour, visible in go test -v.
func TestItemCanSelect(t *testing.T) {
	cases := []struct {
		scenario string
		given    Item   // Given: this item
		wantOK   bool   // Then: can it be selected
		wantWhy  string // Then: the reason key if not ("" means no reason)
	}{
		{
			scenario: "idle cache can be selected",
			given:    Item{Name: "DerivedData"}, // Status not set = StatusIdle
			wantOK:   true,
		},
		{
			scenario: "running container cannot be selected",
			given:    Item{Name: "redis:8", Status: StatusRunning},
			wantWhy:  ReasonRunning,
		},
		{
			scenario: "image used by a container cannot be selected",
			given:    Item{Name: "postgres:17", Status: StatusInUse},
			wantWhy:  ReasonInUse,
		},
		{
			// A real case from 08.11.2026: three iOS 27.0 entries sharing one image.
			scenario: "runtime sharing an image with the active one is protected",
			given: Item{
				Name: "iOS 27.0 (unusable copy)",
				Protected: &Reason{
					Key:  "simulators.shared_image",
					Args: map[string]any{"Runtime": "iOS 27.0"},
				},
			},
			wantWhy: "simulators.shared_image",
		},
		{
			scenario: "busy item says who uses it",
			given: Item{
				Name:   "android-35 system image",
				Status: StatusInUse,
				Busy:   &Reason{Key: "android.used_by_emulator", Args: map[string]any{"Emulator": "Lingary_Test"}},
			},
			wantWhy: "android.used_by_emulator",
		},
		{
			scenario: "busy note does not block an idle item",
			given:    Item{Name: "old cache", Busy: &Reason{Key: "stale.note"}},
			wantOK:   true,
		},
		{
			scenario: "protection is reported before running status",
			given: Item{
				Name:      "travel-planner worktree",
				Status:    StatusRunning,
				Protected: &Reason{Key: "worktrees.unmerged_commits"},
			},
			wantWhy: "worktrees.unmerged_commits",
		},
	}

	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			// When: we ask the core whether the item can be selected
			ok, why := c.given.CanSelect()

			// Then: the answer matches the expected one.
			// Fatalf stops the scenario: if ok is wrong, checking the reason is pointless.
			if ok != c.wantOK {
				t.Fatalf("CanSelect() ok = %v, want %v", ok, c.wantOK)
			}

			gotWhy := ""
			if why != nil {
				gotWhy = why.Key
			}
			// Errorf marks a failure but lets the scenario run to the end.
			if gotWhy != c.wantWhy {
				t.Errorf("reason = %q, want %q", gotWhy, c.wantWhy)
			}
		})
	}
}
