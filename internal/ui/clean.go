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

// providers — все провайдеры в порядке списка слева.
func (u *App) providers() []core.Provider {
	out := make([]core.Provider, len(u.states))
	for i, st := range u.states {
		out[i] = st.provider
	}
	return out
}

// reviewAndClean строит план и показывает его ДО выполнения:
// каждую команду и каждый путь, ровно как они будут выполнены.
func (u *App) reviewAndClean() {
	plan, err := core.BuildPlan(u.sel, u.providers())
	if err != nil {
		dialog.ShowInformation(u.t("error.title"), u.errText(err), u.win)
		return
	}

	actions := plan.Actions()
	content := container.NewVBox(secondary(u.t(plural("confirm.intro", len(actions)), map[string]any{
		"Count": len(actions),
		"Size":  plan.Total().String(),
	}), true))

	for _, step := range plan.Steps {
		icon, tint := u.look(step.Provider)
		name := text(u.t(step.Provider.ID()+".name"), theme.SizeNameText, theme.ColorNameForeground, true)
		content.Add(container.NewHBox(badge(icon, tint, 18), container.NewCenter(name)))

		lines := make([]string, len(step.Actions))
		for i, a := range step.Actions {
			lines[i] = a.String()
		}
		commands := widget.NewLabelWithStyle(strings.Join(lines, "\n"), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
		commands.Selectable = true // можно выделить и скопировать в терминал

		// Команды — в отдельном блоке, как код в документации Apple.
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

	// Scroll — в обе стороны: команды бывают длинными.
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

// dryRun — исполнитель для режима «только показать»: ничего не делает,
// только немного ждёт, чтобы был виден прогресс.
type dryRun struct{}

func (dryRun) Run(ctx context.Context, _ core.Action) error {
	select {
	case <-time.After(150 * time.Millisecond): // подождать 150 мс…
		return nil
	case <-ctx.Done(): // …или выйти сразу, если нажали «Стоп»
		return ctx.Err()
	}
}

// execute выполняет план в фоне, показывая прогресс, а потом итог.
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
		cancel() // освободить ресурсы контекста

		fyne.Do(func() {
			progress.Hide()

			freeAfter, _, _ := diskutil.FreeSpace("/")
			u.showResult(plan, errs, freeAfter-freeBefore, dry)

			// Пересканировать то, что чистили: показать, что осталось.
			u.sel.Clear()
			for _, step := range plan.Steps {
				for _, st := range u.states {
					if st.provider == step.Provider {
						u.scan(st)
					}
				}
			}
			u.fillHeader() // обновить свободное место и полосу диска
		})
	}()
}

// showResult — итог: сколько освободилось или какие действия не удались.
func (u *App) showResult(plan core.Plan, errs []error, freed core.Bytes, dry bool) {
	if len(errs) == 0 {
		msg := u.t("result.ok", map[string]any{"Freed": max(freed, 0).String(), "Planned": plan.Total().String()})
		if dry {
			msg = u.t("result.dry_run")
		} else if plan.UsesTrash() {
			// Файлы из «Загрузок» лежат в Корзине — место ещё не освободилось.
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
