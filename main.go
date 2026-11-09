// Disk Cleaner — приложение для macOS, которое находит то, что занимает
// место и может быть безопасно удалено: кэши Xcode и менеджеров пакетов,
// Docker, симуляторы, эмуляторы Android, node_modules в старых worktree.
//
// main.go — «точка сборки»: единственное место, которое знает обо всех
// провайдерах. Чтобы убрать провайдер — удалите его строку ниже и его
// папку в internal/providers. Больше ничего менять не нужно.
package main

import (
	"log"

	"fyne.io/fyne/v2/app"

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
	"disk-cleaner/internal/ui"
)

// appID — уникальный идентификатор приложения. Нужен, чтобы macOS хранила
// его настройки отдельно от других программ.
const appID = "io.github.mrwd.diskcleaner"

func main() {
	a := app.NewWithID(appID)
	store := settings.New(a.Preferences(), kit.Home())

	ex, err := executor.New()
	if err != nil {
		log.Fatal(err) // напечатать ошибку и завершить программу
	}

	// Порядок здесь — порядок в списке слева.
	providers := []core.Provider{
		worktrees.New(store.Roots), // store.Roots без скобок: передаём функцию, а не результат
		projects.New(store.Roots),
		xcode.New(),
		simulators.New(),
		android.New(),
		docker.New(),
		models.New(),
		devtools.New(),
		packagecaches.New(),
		appcaches.New(),
		downloads.New(), // спросит у macOS доступ к «Загрузкам»
	}

	if err := ui.Run(a, ui.Options{Providers: providers, Executor: ex, Settings: store}); err != nil {
		log.Fatal(err)
	}
}
