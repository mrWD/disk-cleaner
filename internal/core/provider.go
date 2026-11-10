package core

import (
	"context"
	"embed"
)

// Provider is one source of junk: Xcode, Docker, npm caches…
// Each lives in its own folder internal/providers/<name> and knows only core.
//
// A Provider deletes nothing itself. It finds items (Scan)
// and describes what to do to delete them (Plan).
// The Executor runs the actions, after the user
// has seen the plan and confirmed it.
//
// An interface in Go is a list of methods. Any type that has
// all these methods is automatically a Provider.
type Provider interface {
	// ID is a short name: "xcode". Category IDs start with it.
	ID() string

	// Categories is what this provider knows how to find.
	Categories() []Category

	// Scan looks for items right now. It can take a while
	// (measuring folder sizes, asking docker), so it takes a ctx:
	// through it the scan can be cancelled or given a time limit.
	Scan(ctx context.Context) ([]Item, error)

	// Plan says what to do to delete these items.
	// It runs nothing itself.
	Plan(items []Item) ([]Action, error)

	// Translations is the provider's texts: translations/en.json files and so on,
	// baked into the program at build time (embed).
	Translations() embed.FS
}

// Executor runs actions. In core there is only the interface (a port).
// The real implementation lives in internal/executor, and tests use a fake
// that deletes nothing and only remembers what it was asked to do.
type Executor interface {
	Run(ctx context.Context, a Action) error
}
