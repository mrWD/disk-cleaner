package core

import (
	"context"
	"errors"
	"fmt"
)

// Plan — что именно будет сделано. Его показывают пользователю ДО
// выполнения: каждую команду и каждый путь.
type Plan struct {
	Steps []PlanStep
}

// PlanStep — часть плана одного провайдера.
type PlanStep struct {
	Provider Provider
	Items    []Item   // что удаляем
	Actions  []Action // как именно
}

// Total — сколько места освободится по плану.
func (p Plan) Total() Bytes {
	var total Bytes
	for _, step := range p.Steps {
		for _, it := range step.Items {
			total += it.Size
		}
	}
	return total
}

// Actions — все действия плана по порядку.
func (p Plan) Actions() []Action {
	var out []Action
	for _, step := range p.Steps {
		out = append(out, step.Actions...) // ... — «развернуть срез в аргументы»
	}
	return out
}

// NeedsAdmin — понадобится ли пароль администратора.
func (p Plan) NeedsAdmin() bool {
	for _, a := range p.Actions() {
		if a.NeedsAdmin {
			return true
		}
	}
	return false
}

// UsesTrash — переносит ли план что-то в Корзину (тогда место освободится
// только после её очистки, и это надо сказать пользователю).
func (p Plan) UsesTrash() bool {
	for _, a := range p.Actions() {
		if a.Trash {
			return true
		}
	}
	return false
}

// ErrEmptyPlan — провайдер не вернул ни одного действия для объектов.
// Так быть не должно: значит, в провайдере ошибка.
var ErrEmptyPlan = errors.New("provider returned no actions")

// BuildPlan — единственный путь от галочек к действиям.
//
// Для каждого провайдера берём его отмеченные объекты и спрашиваем,
// что с ними сделать. Каждый объект ещё раз проверяется правилом
// CanSelect: защита в глубину на случай, если что-то запрещённое
// всё-таки оказалось в выборе.
func BuildPlan(sel *Selection, providers []Provider) (Plan, error) {
	var plan Plan

	for _, p := range providers {
		items := sel.ItemsFor(p)
		if len(items) == 0 {
			continue // continue — сразу к следующему провайдеру
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

// Progress — сообщение о ходе выполнения плана.
type Progress struct {
	Done   int    // сколько действий уже выполнено (включая это)
	Total  int    // сколько всего
	Action Action // какое действие только что выполнилось
	Err    error  // его ошибка; nil — успешно
}

// Execute выполняет план по одному действию и после каждого сообщает
// о прогрессе через onProgress.
//
// Ошибка одного действия не останавливает остальные (одна упрямая папка
// не должна мешать почистить всё остальное) — все ошибки вернутся списком.
// Отмена через ctx останавливает выполнение перед следующим действием.
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
