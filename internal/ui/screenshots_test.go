//go:build screenshots

// Инструмент, а не обычный тест: снимки всех экранов приложения на реальных
// данных этого Mac, без открытия окна (Fyne умеет рисовать «в памяти»).
// Обычный go test его пропускает из-за строки go:build выше.
//
// Запуск из корня проекта (картинки появятся в папке screenshots/):
//
//	go test -tags screenshots -run TestScreenshots ./internal/ui/ -args -lang=ru -dark
//
// Тест импортирует провайдеры — это можно только в тестах: сам пакет ui
// о провайдерах не знает.
package ui

import (
	"context"
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"disk-cleaner/internal/core"
	"disk-cleaner/internal/executor"
	"disk-cleaner/internal/providers/android"
	"disk-cleaner/internal/providers/appcaches"
	"disk-cleaner/internal/providers/devtools"
	"disk-cleaner/internal/providers/docker"
	"disk-cleaner/internal/providers/downloads"
	"disk-cleaner/internal/providers/kit"
	"disk-cleaner/internal/providers/models"
	"disk-cleaner/internal/providers/packagecaches"
	"disk-cleaner/internal/providers/projects"
	"disk-cleaner/internal/providers/simulators"
	"disk-cleaner/internal/providers/worktrees"
	"disk-cleaner/internal/providers/xcode"
	"disk-cleaner/internal/settings"
)

var (
	shotLang = flag.String("lang", "en", "язык интерфейса: en или ru")
	shotDir  = flag.String("out", "../../screenshots", "куда сохранить картинки")
	shotDark = flag.Bool("dark", false, "снимать в тёмном виде")
)

// frozen — провайдер, чьё сканирование в фоне никогда не заканчивается.
// Результаты мы получаем заранее и подставляем сами: так фоновые горутины
// не мешают снимать экраны.
type frozen struct{ core.Provider }

// Look передаёт значок и цвет настоящего провайдера: встроенный интерфейс
// даёт только методы core.Provider, а Look в него не входит.
func (f frozen) Look() ([]byte, string) {
	if l, ok := f.Provider.(core.Look); ok {
		return l.Look()
	}
	return nil, "#8E8E93"
}

func (frozen) Scan(ctx context.Context) ([]core.Item, error) {
	<-ctx.Done() // ждать вечно (контекст фоновый и не отменяется)
	return nil, ctx.Err()
}

func TestScreenshots(t *testing.T) {
	if err := os.MkdirAll(*shotDir, 0o755); err != nil {
		t.Fatal(err)
	}

	a := test.NewTempApp(t)
	store := settings.New(a.Preferences(), kit.Home())
	store.SetLanguage(*shotLang)
	ex, _ := executor.New()

	real := []core.Provider{
		worktrees.New(store.Roots), projects.New(store.Roots), xcode.New(), simulators.New(),
		android.New(), docker.New(), models.New(), devtools.New(),
		packagecaches.New(), appcaches.New(), downloads.New(),
	}

	// Настоящее сканирование — заранее и по очереди.
	type result struct {
		items []core.Item
		err   error
	}
	results := make([]result, len(real))
	wrapped := make([]core.Provider, len(real))
	for i, p := range real {
		items, err := p.Scan(context.Background())
		results[i] = result{items, err}
		wrapped[i] = frozen{p}
	}

	u, err := New(a, Options{Providers: wrapped, Executor: ex, Settings: store})
	if err != nil {
		t.Fatal(err)
	}
	for i, st := range u.states {
		st.scanning, st.items, st.err = false, results[i].items, results[i].err
	}
	if *shotDark {
		u.applyAppearance(appearanceDark) // так же, как кнопка в шапке
	}
	u.win.Resize(fyne.NewSize(1180, 780))
	u.render()

	save := func(name string) {
		variant := "light"
		if *shotDark {
			variant = "dark"
		}
		f, err := os.Create(filepath.Join(*shotDir, *shotLang+"-"+variant+"-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, u.win.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}

	// По снимку на каждого провайдера.
	for i, st := range u.states {
		u.selectProvider(i)
		save(st.provider.ID())
	}

	// Развёрнутая категория: объяснения и подробности.
	u.selectProvider(0)
	if cats := u.states[0].provider.Categories(); len(cats) > 0 {
		u.expanded[cats[0].ID] = true
		u.refreshDetails()
		save("expanded")
		u.expanded[cats[0].ID] = false
	}

	// Поиск в разделе и глобальный поиск.
	u.setSectionQuery("lingary")
	u.relayout()
	save("section-search")
	u.searchEntry.SetText("lingary")
	u.relayout()
	save("global-search")
	u.searchEntry.SetText("")

	// Настройки с выбором оформления; потом закрываем окно настроек.
	u.showSettings()
	save("settings")
	u.win.Canvas().Overlays().Remove(u.win.Canvas().Overlays().Top())

	// Окно подтверждения: выбираем до трёх объектов первого провайдера.
	u.selectProvider(0)
	for _, it := range u.states[0].items {
		if ok, _ := it.CanSelect(); ok && u.sel.Len() < 3 {
			_ = u.sel.Add(it)
		}
	}
	u.refresh()
	u.reviewAndClean()
	save("confirm")
}
