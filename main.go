// Disk Cleaner is a macOS app that finds what takes up space and can be
// deleted safely: Xcode and package manager caches, Docker, simulators,
// Android emulators, node_modules in old worktrees.
//
// main.go is the "assembly point": the only place that knows about all the
// providers. To remove a provider, delete its line below and its folder
// in internal/providers. Nothing else needs to change.
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
	"disk-cleaner/internal/providers/gitscan"
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

// appID is the app's unique identifier. It is needed so that macOS keeps
// the app's settings separate from other programs.
const appID = "io.github.mrwd.diskcleaner"

func main() {
	a := app.NewWithID(appID)
	store := settings.New(a.Preferences(), kit.Home())

	ex, err := executor.New()
	if err != nil {
		log.Fatal(err) // print the error and exit the program
	}

	// The order here is the order in the list on the left.
	providers := []core.Provider{
		worktrees.New(store.Roots), // store.Roots without parentheses: we pass the function, not its result
		projects.New(store.Roots),
		xcode.New(),
		simulators.New(),
		android.New(),
		docker.New(),
		models.New(),
		devtools.New(),
		packagecaches.New(),
		appcaches.New(),
		downloads.New(store.UseDownloads), // will ask macOS for access to Downloads, if it is turned on in settings
	}

	if err := ui.Run(a, ui.Options{
		Providers: providers,
		Executor:  ex,
		Settings:  store,
		// Suggestions for the project folders: folders in ~ with git repositories.
		FindProjectFolders: func() []string { return gitscan.ProjectFolders(kit.Home()) },
	}); err != nil {
		log.Fatal(err)
	}
}
