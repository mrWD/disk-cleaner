package ui

import (
	"context"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/diskutil"
)

// providers lists all providers in the order of the list on the left.
func (u *App) providers() []core.Provider {
	out := make([]core.Provider, len(u.states))
	for i, st := range u.states {
		out[i] = st.provider
	}
	return out
}

// reviewAndClean is the "Review & Clean…" button: a plan for what is selected.
func (u *App) reviewAndClean() {
	u.review(u.sel, "")
}

// safeItems is what in a section can be deleted without a second thought: items from
// Safe categories that can be selected (not running, not busy, not
// protected). While the section is scanning or has an error, nothing.
func safeItems(st *providerState) []core.Item {
	if st.scanning || st.err != nil {
		return nil
	}

	var out []core.Item
	for _, cat := range st.provider.Categories() {
		if cat.Risk != core.RiskSafe {
			continue // "Caution" only if the person selects it themselves
		}
		for _, it := range itemsOf(st.items, cat.ID) {
			if ok, _ := it.CanSelect(); ok {
				out = append(out, it)
			}
		}
	}
	return out
}

// safeSelection is a separate selection of everything safe in the sections states.
// Separate, not u.sel: the "Clean All Safe" button doesn't touch the checkmarks on screen,
// and if you press "Cancel" in the review window, everything stays as it was.
func (u *App) safeSelection(states ...*providerState) *core.Selection {
	sel := core.NewSelection()
	for _, st := range states {
		for _, it := range safeItems(st) {
			_ = sel.Add(it) // there can be no error: safeItems returns only what can be selected
		}
	}
	return sel
}

// cleanAll handles the "Clean All Safe" buttons: in one section (a single state) or in all of them.
// It doesn't delete right away: first the same review window with the list of commands.
func (u *App) cleanAll(states ...*providerState) {
	sel := u.safeSelection(states...)
	if sel.Len() == 0 {
		return
	}
	u.review(sel, u.t("confirm.safe_only"))
}

// review builds the plan for sel and shows it BEFORE it runs:
// every command and every path, exactly as they will be executed.
// note is an explanation under the first line (for "Clean All Safe" it says what is included).
func (u *App) review(sel *core.Selection, note string) {
	var notes []string
	if note != "" {
		notes = append(notes, note)
	}

	// The administrator password is turned off in Settings: leave out what needs it.
	if !u.opts.Settings.AllowAdmin() {
		var skipped int
		sel, skipped = u.withoutAdmin(sel)
		if sel.Len() == 0 {
			dialog.ShowInformation(u.t("error.title"), u.t("admin.all_skipped"), u.win)
			return
		}
		if skipped > 0 {
			notes = append(notes, u.t(plural("admin.skipped", skipped), map[string]any{"Count": skipped}))
		}
	}

	plan, err := core.BuildPlan(sel, u.providers())
	if err != nil {
		dialog.ShowInformation(u.t("error.title"), u.errText(err), u.win)
		return
	}

	actions := plan.Actions()
	content := container.NewVBox(secondary(u.t(plural("confirm.intro", len(actions)), map[string]any{
		"Count": len(actions),
		"Size":  plan.Total().String(),
	}), true))
	for _, n := range notes {
		content.Add(secondary(n, true))
	}

	for _, step := range plan.Steps {
		icon, tint := u.look(step.Provider)
		name := text(u.t(step.Provider.ID()+".name"), theme.SizeNameText, theme.ColorNameForeground, true)
		content.Add(container.NewHBox(badge(icon, tint, 18), container.NewCenter(name)))

		lines := make([]string, len(step.Actions))
		for i, a := range step.Actions {
			lines[i] = a.String()
		}
		commands := widget.NewLabelWithStyle(strings.Join(lines, "\n"), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
		commands.Selectable = true // can be selected and copied into a terminal

		// Commands go in a separate block, like code in Apple's documentation.
		box := canvas.NewRectangle(theme.Color(colorCode))
		box.CornerRadius = 8
		content.Add(container.NewStack(box, commands))
	}

	if plan.NeedsAdmin() {
		content.Add(colored(u.t("confirm.admin"), theme.ColorNameWarning))
	}
	if u.opts.Settings.DryRun() {
		content.Add(colored(u.t("confirm.dry_run"), theme.ColorNameWarning))
	}

	// Scroll in both directions: commands can be long.
	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(760, 420))

	d := dialog.NewCustomConfirm(u.t("confirm.title"), u.t("confirm.ok"), u.t("confirm.cancel"), scroll,
		func(ok bool) {
			if ok {
				u.execute(plan)
			}
		}, u.win)
	d.Show()
}

// dryRun is the executor for dry run mode: it does nothing,
// it only waits a little so that progress is visible.
type dryRun struct{}

func (dryRun) Run(ctx context.Context, _ core.Action) error {
	select {
	case <-time.After(150 * time.Millisecond): // wait 150 ms...
		return nil
	case <-ctx.Done(): // ...or leave at once if "Stop" was pressed
		return ctx.Err()
	}
}

// execute runs the plan in the background, showing progress, and then the result.
func (u *App) execute(plan core.Plan) {
	freeBefore, _, _ := diskutil.FreeSpace("/")

	status := widget.NewLabel("")
	status.Truncation = fyne.TextTruncateEllipsis
	bar := widget.NewProgressBar()

	ctx, cancel := context.WithCancel(context.Background())
	stop := widget.NewButton(u.t("progress.cancel"), cancel)

	progress := dialog.NewCustomWithoutButtons(u.t("progress.title"), container.NewVBox(status, bar, stop), u.win)
	progress.Resize(fyne.NewSize(620, 180))
	progress.Show()

	var ex core.Executor = u.opts.Executor
	dry := u.opts.Settings.DryRun()
	if dry {
		ex = dryRun{}
	}

	go func() {
		errs := core.Execute(ctx, plan, ex, func(p core.Progress) {
			fyne.Do(func() {
				bar.SetValue(float64(p.Done) / float64(p.Total))
				status.SetText(p.Action.String())
			})
		})
		cancel() // release the context's resources

		fyne.Do(func() {
			progress.Hide()

			freeAfter, _, _ := diskutil.FreeSpace("/")
			u.showResult(plan, errs, freeAfter-freeBefore, dry)

			// Rescan what was cleaned: show what is left.
			u.sel.Clear()
			for _, step := range plan.Steps {
				for _, st := range u.states {
					if st.provider == step.Provider {
						u.scan(st)
					}
				}
			}
			u.fillDisk() // update the free space and the disk bar
		})
	}()
}

// showResult shows the result: how much was freed or which actions failed.
func (u *App) showResult(plan core.Plan, errs []error, freed core.Bytes, dry bool) {
	if len(errs) == 0 {
		msg := u.t("result.ok", map[string]any{"Freed": max(freed, 0).String(), "Planned": plan.Total().String()})
		if dry {
			msg = u.t("result.dry_run")
		} else if plan.UsesTrash() {
			// Files from Downloads are in the Trash, so the space is not freed yet.
			msg += "\n\n" + u.t("result.trash_note")
		}
		dialog.ShowInformation(u.t("result.title"), msg, u.win)
		return
	}

	lines := make([]string, len(errs))
	for i, err := range errs {
		lines[i] = err.Error()
	}
	list := widget.NewLabelWithStyle(strings.Join(lines, "\n"), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	list.Selectable = true

	scroll := container.NewScroll(list)
	scroll.SetMinSize(fyne.NewSize(700, 260))

	content := container.NewVBox(u.wrapLabel(u.t("result.errors", map[string]any{
		"Failed": len(errs),
		"Total":  len(plan.Actions()),
	})), scroll)

	dialog.ShowCustom(u.t("result.errors_title"), u.t("result.close"), content, u.win)
}
