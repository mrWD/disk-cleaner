package core

import (
	"context"
	"errors"
	"fmt"
)

// Plan is exactly what is going to be done. It is shown to the user BEFORE
// anything runs: every command and every path.
type Plan struct {
	Steps []PlanStep
}

// PlanStep is the part of the plan that belongs to one provider.
type PlanStep struct {
	Provider Provider
	Items    []Item   // what we delete
	Actions  []Action // how exactly
}

// Total is how much space the plan will free.
func (p Plan) Total() Bytes {
	var total Bytes
	for _, step := range p.Steps {
		for _, it := range step.Items {
			total += it.Size
		}
	}
	return total
}

// Actions returns all the actions of the plan, in order.
func (p Plan) Actions() []Action {
	var out []Action
	for _, step := range p.Steps {
		out = append(out, step.Actions...) // ... means "spread the slice into arguments"
	}
	return out
}

// NeedsAdmin tells whether the administrator password will be needed.
func (p Plan) NeedsAdmin() bool {
	for _, a := range p.Actions() {
		if a.NeedsAdmin {
			return true
		}
	}
	return false
}

// UsesTrash tells whether the plan moves anything to the Trash (then the space
// is freed only after the Trash is emptied, and the user must be told so).
func (p Plan) UsesTrash() bool {
	for _, a := range p.Actions() {
		if a.Trash {
			return true
		}
	}
	return false
}

// ErrEmptyPlan means the provider returned no actions for the items.
// That must never happen: it means there is a bug in the provider.
var ErrEmptyPlan = errors.New("provider returned no actions")

// BuildPlan is the only path from checkmarks to actions.
//
// For each provider we take its selected items and ask what to do
// with them. Every item is checked once more with the CanSelect rule:
// defence in depth, in case something forbidden still ended up
// in the selection.
func BuildPlan(sel *Selection, providers []Provider) (Plan, error) {
	var plan Plan

	for _, p := range providers {
		items := sel.ItemsFor(p)
		if len(items) == 0 {
			continue // continue jumps straight to the next provider
		}

		for _, it := range items {
			if ok, why := it.CanSelect(); !ok {
				return Plan{}, &BlockedError{Item: it, Reason: *why}
			}
		}

		actions, err := p.Plan(items)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", p.ID(), err)
		}
		if len(actions) == 0 {
			return Plan{}, fmt.Errorf("%s: %w", p.ID(), ErrEmptyPlan)
		}

		plan.Steps = append(plan.Steps, PlanStep{Provider: p, Items: items, Actions: actions})
	}

	return plan, nil
}

// Progress is a message about how far the plan has got.
type Progress struct {
	Done   int    // how many actions are done (including this one)
	Total  int    // how many in total
	Action Action // which action just finished
	Err    error  // its error; nil means success
}

// Execute runs the plan one action at a time and reports progress
// through onProgress after each one.
//
// An error in one action doesn't stop the others (one stubborn folder
// must not prevent cleaning everything else); all the errors are returned as a list.
// Cancelling through ctx stops the run before the next action.
func Execute(ctx context.Context, plan Plan, ex Executor, onProgress func(Progress)) []error {
	actions := plan.Actions()
	var errs []error

	for i, a := range actions {
		if err := ctx.Err(); err != nil {
			return append(errs, err)
		}

		err := ex.Run(ctx, a)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a, err))
		}

		if onProgress != nil {
			onProgress(Progress{Done: i + 1, Total: len(actions), Action: a, Err: err})
		}
	}

	return errs
}
