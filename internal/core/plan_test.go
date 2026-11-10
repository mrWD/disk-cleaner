package core

import (
	"context"
	"embed"
	"errors"
	"testing"
)

// fakeProvider is a provider for tests. It looks for nothing on disk:
// the items are set right in the test, and Plan deletes each item by its ID.
type fakeProvider struct {
	id      string
	planErr error // if not nil, Plan returns this error
	noPlan  bool  // if true, Plan returns an empty list (a provider bug)
}

// The five methods below make fakeProvider a provider: the Provider interface
// is satisfied automatically, Go has no "implements" keyword.
func (f fakeProvider) ID() string { return f.id }

func (f fakeProvider) Categories() []Category {
	return []Category{{ID: f.id + ".cache", Risk: RiskSafe}}
}

func (f fakeProvider) Scan(context.Context) ([]Item, error) { return nil, nil }

func (f fakeProvider) Plan(items []Item) ([]Action, error) {
	if f.planErr != nil {
		return nil, f.planErr
	}
	if f.noPlan {
		return nil, nil
	}

	var actions []Action
	for _, it := range items {
		actions = append(actions, Action{RemovePath: it.ID})
	}
	return actions, nil
}

func (f fakeProvider) Translations() embed.FS { return embed.FS{} }

// fakeExecutor deletes nothing: it records what it was asked to do
// and "fails" on the paths listed in failOn.
type fakeExecutor struct {
	ran    []string
	failOn map[string]bool
}

// Pointer receiver: the method must change the ran field of the executor itself.
func (f *fakeExecutor) Run(_ context.Context, a Action) error {
	f.ran = append(f.ran, a.RemovePath)
	if f.failOn[a.RemovePath] {
		return errors.New("permission denied")
	}
	return nil
}

// item is a short way to create an item of the "<provider>.cache" category.
func item(provider, path string, size Bytes) Item {
	return Item{ID: path, CategoryID: provider + ".cache", Name: path, Size: size}
}

func TestBuildPlan(t *testing.T) {
	xcode := fakeProvider{id: "xcode"}
	npm := fakeProvider{id: "npm"}
	providers := []Provider{xcode, npm}

	t.Run("empty selection gives an empty plan", func(t *testing.T) {
		plan, err := BuildPlan(NewSelection(), providers)

		if err != nil || len(plan.Steps) != 0 {
			t.Errorf("plan = %+v, err = %v; want empty", plan, err)
		}
	})

	t.Run("each provider plans only its own items", func(t *testing.T) {
		// Given: items of two providers are selected
		sel := NewSelection()
		mustAdd(t, sel, item("xcode", "/x/DerivedData", 100))
		mustAdd(t, sel, item("npm", "/n/_cacache", 50))

		// When: we build the plan
		plan, err := BuildPlan(sel, providers)

		// Then: two steps, each with its own action, the total size is the sum
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Steps) != 2 {
			t.Fatalf("steps = %d, want 2", len(plan.Steps))
		}
		if plan.Steps[0].Actions[0].RemovePath != "/x/DerivedData" ||
			plan.Steps[1].Actions[0].RemovePath != "/n/_cacache" {
			t.Errorf("actions = %v", plan.Actions())
		}
		if plan.Total() != 150 {
			t.Errorf("Total() = %s, want 150 B", plan.Total())
		}
	})

	t.Run("item that is blocked now is refused even if it got into the selection", func(t *testing.T) {
		// Given: a running item got into the selection bypassing Add
		// (the test is in the same package and can touch the private items field)
		sel := NewSelection()
		running := item("xcode", "/x/running", 10)
		running.Status = StatusRunning
		sel.items[keyOf(running)] = running

		// When: we build the plan
		_, err := BuildPlan(sel, providers)

		// Then: no plan is built, the reason is "running"
		var blocked *BlockedError
		if !errors.As(err, &blocked) || blocked.Reason.Key != ReasonRunning {
			t.Errorf("err = %v, want *BlockedError(running)", err)
		}
	})

	t.Run("provider error names the provider", func(t *testing.T) {
		// Given: a provider that cannot make a plan
		broken := fakeProvider{id: "broken", planErr: errors.New("docker is not running")}
		sel := NewSelection()
		mustAdd(t, sel, item("broken", "/b/x", 1))

		// When
		_, err := BuildPlan(sel, []Provider{broken})

		// Then: the error shows which provider failed
		if err == nil || err.Error() != "broken: docker is not running" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("provider that returns no actions is a bug, not a silent no-op", func(t *testing.T) {
		silent := fakeProvider{id: "silent", noPlan: true}
		sel := NewSelection()
		mustAdd(t, sel, item("silent", "/s/x", 1))

		_, err := BuildPlan(sel, []Provider{silent})

		if !errors.Is(err, ErrEmptyPlan) {
			t.Errorf("err = %v, want ErrEmptyPlan", err)
		}
	})
}

func TestExecute(t *testing.T) {
	plan := Plan{Steps: []PlanStep{{
		Provider: fakeProvider{id: "xcode"},
		Actions:  []Action{{RemovePath: "/a"}, {RemovePath: "/b"}, {RemovePath: "/c"}},
	}}}

	t.Run("all actions run in order and progress is reported", func(t *testing.T) {
		ex := &fakeExecutor{}
		var reports []int

		errs := Execute(context.Background(), plan, ex, func(p Progress) {
			reports = append(reports, p.Done)
		})

		if len(errs) != 0 {
			t.Fatalf("errs = %v", errs)
		}
		if len(ex.ran) != 3 || ex.ran[0] != "/a" || ex.ran[2] != "/c" {
			t.Errorf("ran = %v", ex.ran)
		}
		if len(reports) != 3 || reports[2] != 3 {
			t.Errorf("progress = %v, want [1 2 3]", reports)
		}
	})

	t.Run("one failed action does not stop the rest", func(t *testing.T) {
		// Given: the second action will fail
		ex := &fakeExecutor{failOn: map[string]bool{"/b": true}}

		// When
		errs := Execute(context.Background(), plan, ex, nil)

		// Then: all three ran, there is one error and it shows the action
		if len(ex.ran) != 3 {
			t.Errorf("ran = %v, want all 3", ex.ran)
		}
		if len(errs) != 1 || errs[0].Error() != "rm -rf /b: permission denied" {
			t.Errorf("errs = %v", errs)
		}
	})

	t.Run("cancel stops before the next action", func(t *testing.T) {
		// Given: the context is cancelled after the first action
		ctx, cancel := context.WithCancel(context.Background())
		ex := &fakeExecutor{}

		// When
		errs := Execute(ctx, plan, ex, func(p Progress) {
			if p.Done == 1 {
				cancel()
			}
		})

		// Then: only the first one ran, and the errors include the cancellation
		if len(ex.ran) != 1 {
			t.Errorf("ran = %v, want only /a", ex.ran)
		}
		if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
			t.Errorf("errs = %v, want context.Canceled", errs)
		}
	})
}
