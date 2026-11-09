package ui

import (
	"context"

	"fyne.io/fyne/v2"
)

// scanAll пересканирует всех провайдеров одновременно.
// Свободное место в шапке обновится вместе с остальным в refresh.
func (u *App) scanAll() {
	for _, st := range u.states {
		u.scan(st)
	}
}

// scan запускает сканирование одного провайдера в фоне.
//
// Сканирование может идти минуту и дольше, а окно в это время должно
// отвечать. Поэтому сама работа идёт в горутине, а результат возвращается
// в окно через fyne.Do: менять виджеты можно только из UI-потока.
func (u *App) scan(st *providerState) {
	// Старые результаты устарели — снимаем отметки с объектов этого провайдера.
	for _, it := range u.sel.ItemsFor(st.provider) {
		u.sel.Remove(it)
	}

	st.gen++
	gen := st.gen // запоминаем номер этого сканирования
	st.scanning = true
	st.err = nil
	u.refresh()

	go func() {
		items, err := st.provider.Scan(context.Background())

		fyne.Do(func() {
			if st.gen != gen {
				return // пока мы искали, запустили новое сканирование — наш результат устарел
			}
			st.scanning = false
			st.items, st.err = items, err
			u.refresh()
		})
	}()
}
