package ui

import (
	"context"
	"errors"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"disk-cleaner/internal/update"
)

// Updates. At launch, if Settings allow it, the app asks GitHub whether
// there's a newer version (internal/update). If there is, a card above the
// disk card says so, and Update… shows what's new and installs it the way
// the app was installed: a copy built from source rebuilds itself in
// Terminal; a downloaded copy downloads the new release, checks its
// signature, and restarts into it.

// Updater is what the window needs from internal/update; tests use a fake.
type Updater interface {
	Version() string                                // the short commit of this copy, "" if unknown
	FromSource() bool                               // built from source rather than downloaded
	Check(ctx context.Context) (update.News, error) // something newer on GitHub?
	Install(ctx context.Context) (quit bool, err error)
}

// checkForUpdates runs the check in the background, if it's allowed. Any
// failure (offline, GitHub busy, a developer's own build) just means no card.
func (u *App) checkForUpdates() {
	if u.opts.Updater == nil || !u.opts.Settings.CheckUpdates() {
		return
	}
	go func() {
		news, err := u.opts.Updater.Check(context.Background())
		fyne.Do(func() {
			if err != nil {
				news = update.News{}
			}
			u.news = news
			u.fillUpdate()
		})
	}()
}

// fillUpdate — the card above the disk card: there's a new version, and the
// button to install it. Nothing while there's nothing new.
func (u *App) fillUpdate() {
	if u.updateBox == nil {
		return
	}
	u.updateBox.Objects = nil

	switch {
	case u.updating:
		u.updateBox.Add(pad(0, 8, 0, 0, card(text(u.t("update.updating"), theme.SizeNameText, colorSecondary, false))))
	case u.news.Count > 0:
		title := text(u.t("update.available"), theme.SizeNameText, theme.ColorNameForeground, true)
		count := text(u.t(plural("update.count", u.news.Count), map[string]any{"Count": u.news.Count}), sizeCaption, colorSecondary, false)
		btn := widget.NewButton(u.t("update.button"), u.showUpdate)
		btn.Importance = widget.HighImportance
		u.updateBox.Add(pad(0, 8, 0, 0, card(container.NewBorder(nil, nil, nil, container.NewCenter(btn), container.NewVBox(title, count)))))
	}

	u.updateBox.Refresh()
	u.relayout()
}

// showUpdate — what's new, how the update works, and the button to start it.
func (u *App) showUpdate() {
	list := container.NewVBox(u.wrapLabel(u.t("update.intro")))
	for _, title := range u.news.Titles {
		list.Add(u.wrapLabel("•  " + title))
	}
	if more := u.news.Count - len(u.news.Titles); more > 0 {
		list.Add(secondary(u.t("update.more", map[string]any{"Count": more}), false))
	}
	howKey := "update.how" // rebuilt from source in Terminal
	if !u.opts.Updater.FromSource() {
		howKey = "update.how_release" // downloaded, checked, swapped in
	}
	how := u.wrapLabel(u.t(howKey))
	how.Importance = widget.LowImportance
	list.Add(gap(0, 6))
	list.Add(how)

	var closeSheet func()
	later := widget.NewButton(u.t("update.later"), func() { closeSheet() })
	install := widget.NewButtonWithIcon(u.t("update.install"), theme.DownloadIcon(), func() {
		closeSheet()
		u.installUpdate()
	})
	install.Importance = widget.HighImportance
	closeSheet = u.showSheet(u.t("update.title"), sheetScroll(list), 560, 460, later, install)
}

// installUpdate starts the update. A copy built from source opens Terminal
// right away; a downloaded copy downloads and checks the release first (a
// few seconds, with a progress window), then quits so it can be replaced.
func (u *App) installUpdate() {
	if u.opts.Updater.FromSource() {
		_, err := u.opts.Updater.Install(context.Background())
		u.afterInstall(false, err)
		return
	}

	bar := widget.NewProgressBarInfinite()
	progress := dialog.NewCustomWithoutButtons(u.t("update.downloading"), container.NewVBox(bar), u.win)
	progress.Resize(fyne.NewSize(420, 120))
	progress.Show()
	go func() {
		quit, err := u.opts.Updater.Install(context.Background())
		fyne.Do(func() {
			progress.Hide()
			u.afterInstall(quit, err)
		})
	}()
}

// afterInstall — what happens once the update has started (or failed to):
// quit so the swap script can replace this copy, show that Terminal is on
// it, or explain why it can't be done automatically and what to run instead.
func (u *App) afterInstall(quit bool, err error) {
	if err == nil {
		if quit {
			u.app.Quit() // the swap script waits for this, then opens the new copy
			return
		}
		u.updating = true
		u.fillUpdate()
		return
	}

	var blocked *update.Blocked
	if !errors.As(err, &blocked) {
		dialog.ShowError(err, u.win)
		return
	}

	// The commands to run by hand: in the known checkout, or a fresh one.
	commands := []string{"git clone https://github.com/mrWD/disk-cleaner.git", "cd disk-cleaner", "./scripts/build.sh --install"}
	if blocked.Dir != "" && blocked.Key != update.BlockedNoSource {
		commands = []string{"cd " + blocked.Dir, "git pull", "./scripts/build.sh --install"}
	}
	code := widget.NewLabelWithStyle(strings.Join(commands, "\n"), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	code.Selectable = true // so it can be copied into Terminal

	content := container.NewVBox(
		u.wrapLabel(u.t(blocked.Key, map[string]any{"Dir": tildePath(blocked.Dir)})),
		u.wrapLabel(u.t("update.manual")),
		container.NewStack(roundedRect(theme.Color(colorCode), 8), code),
	)
	d := dialog.NewCustom(u.t("update.manual_title"), u.t("result.close"), content, u.win)
	d.Resize(fyne.NewSize(560, 0))
	d.Show()
}

// updatesSection — the Updates part of Settings: the switch for checking at
// launch, and Check Now with its result. Returns the widgets and the switch
// (saved with the other settings).
func (u *App) updatesSection() (fyne.CanvasObject, *widget.Check) {
	check := widget.NewCheck(u.t("settings.updates.check"), nil)
	check.SetChecked(u.opts.Settings.CheckUpdates())
	hint := u.wrapLabel(u.t("settings.updates.hint"))
	hint.Importance = widget.LowImportance

	status := widget.NewLabel("")
	status.Importance = widget.LowImportance
	status.Wrapping = fyne.TextWrapWord

	now := link(u.t("settings.updates.now"), func() {
		status.SetText(u.t("perm.status.checking"))
		u.refreshOverlays()
		go func() {
			news, err := u.opts.Updater.Check(context.Background())
			fyne.Do(func() {
				switch {
				case errors.Is(err, update.ErrUnknownBuild):
					status.SetText(u.t("settings.updates.own_build"))
				case err != nil:
					status.SetText(u.t("settings.updates.failed"))
				case news.Count == 0:
					status.SetText(u.t("settings.updates.up_to_date"))
				default:
					status.SetText(u.t(plural("settings.updates.found", news.Count), map[string]any{"Count": news.Count}))
				}
				if err == nil {
					u.news = news
					u.fillUpdate()
				}
				u.refreshOverlays()
			})
		}()
	})

	return container.NewVBox(check, hint, container.NewHBox(now), status), check
}
