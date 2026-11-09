package core

import (
	"context"
	"embed"
)

// Provider — один источник мусора: Xcode, Docker, кэши npm…
// Каждый живёт в своей папке internal/providers/<имя> и знает только core.
//
// Provider ничего не удаляет сам. Он находит объекты (Scan)
// и описывает, что сделать, чтобы их удалить (Plan).
// Выполняет действия Executor — после того как пользователь
// увидел план и подтвердил.
//
// interface в Go — это список методов. Любой тип, у которого
// есть все эти методы, автоматически считается Provider.
type Provider interface {
	// ID — короткое имя: "xcode". С него начинаются ID категорий.
	ID() string

	// Categories — что этот провайдер умеет находить.
	Categories() []Category

	// Scan ищет объекты прямо сейчас. Может занять время
	// (посчитать размер папок, спросить docker), поэтому принимает ctx:
	// через него сканирование можно отменить или ограничить по времени.
	Scan(ctx context.Context) ([]Item, error)

	// Plan говорит, что сделать, чтобы удалить эти объекты.
	// Сам ничего не выполняет.
	Plan(items []Item) ([]Action, error)

	// Translations — тексты провайдера: файлы translations/en.json и т.д.,
	// вшитые в программу при сборке (embed).
	Translations() embed.FS
}

// Executor выполняет действия. В core — только интерфейс (порт).
// Настоящая реализация будет в internal/executor, а в тестах — фейк,
// который ничего не удаляет, а только запоминает, что его попросили.
type Executor interface {
	Run(ctx context.Context, a Action) error
}
