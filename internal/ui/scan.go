package ui

import (
	"context"

	"fyne.io/fyne/v2"
)

// scanAll rescans all providers at the same time.
// The free space in the header is updated together with everything else in refresh.
func (u *App) scanAll() {
	for _, st := range u.states {
		u.scan(st)
	}
}

// scan starts scanning one provider in the background.
//
// A scan can take a minute or longer, and the window has to keep
// responding in the meantime. So the work itself runs in a goroutine, and the result goes back
// to the window through fyne.Do: widgets may only be changed from the UI thread.
func (u *App) scan(st *providerState) {
	// The old results are stale, so we clear the selection marks on this provider's items.
	for _, it := range u.sel.ItemsFor(st.provider) {
		u.sel.Remove(it)
	}

	st.gen++
	gen := st.gen // remember this scan's number
	st.scanning = true
	st.err = nil
	u.refresh()

	go func() {
		items, err := st.provider.Scan(context.Background())

		fyne.Do(func() {
			if st.gen != gen {
				return // while we were searching, a new scan was started, so our result is stale
			}
			st.scanning = false
			u.markExcluded(items) // the "Never clean" list, see never.go
			st.items, st.err = items, err
			u.refresh()
		})
	}()
}
