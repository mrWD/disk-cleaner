package core

import (
	"errors"
	"testing"
)

// Объекты для сценариев. var на уровне пакета видны во всех тестах core.
var (
	derivedData = Item{ID: "~/Library/Developer/Xcode/DerivedData", CategoryID: "xcode.derived_data", Name: "DerivedData", Size: 1_500_000_000}
	npmCache    = Item{ID: "~/.npm/_cacache", CategoryID: "packagecaches.npm", Name: "npm cache", Size: 500_000_000}
	redis       = Item{ID: "a1b2c3", CategoryID: "docker.containers", Name: "redis:8", Size: 98_000_000, Status: StatusRunning}
)

// mustAdd добавляет объект и роняет сценарий, если не получилось.
// t.Helper() говорит Go: при ошибке показывай строку теста, который
// вызвал mustAdd, а не строку внутри mustAdd.
func mustAdd(t *testing.T, s *Selection, it Item) {
	t.Helper()

	if err := s.Add(it); err != nil {
		t.Fatalf("Add(%q) failed: %v", it.Name, err)
	}
}

func TestSelection(t *testing.T) {
	t.Run("empty selection has nothing to clean", func(t *testing.T) {
		// Given: новый выбор
		s := NewSelection()

		// Then: в нём ничего нет
		if s.Len() != 0 || s.Total() != 0 {
			t.Errorf("Len() = %d, Total() = %s, want 0 and 0 B", s.Len(), s.Total())
		}
	})

	t.Run("selecting idle items sums their size", func(t *testing.T) {
		// Given: новый выбор
		s := NewSelection()

		// When: отмечаем два свободных объекта
		mustAdd(t, s, derivedData)
		mustAdd(t, s, npmCache)

		// Then: оба в выборе, размер — их сумма
		if s.Len() != 2 {
			t.Errorf("Len() = %d, want 2", s.Len())
		}
		if s.Total() != 2_000_000_000 {
			t.Errorf("Total() = %s, want 2.0 GB", s.Total())
		}
	})

	t.Run("running item is refused with a reason", func(t *testing.T) {
		// Given: новый выбор
		s := NewSelection()

		// When: пытаемся отметить запущенный контейнер
		err := s.Add(redis)

		// Then: ошибка именно *BlockedError с причиной "running"…
		// errors.As проверяет тип ошибки и кладёт её в blocked.
		var blocked *BlockedError
		if !errors.As(err, &blocked) {
			t.Fatalf("Add(redis) error = %v, want *BlockedError", err)
		}
		if blocked.Reason.Key != ReasonRunning {
			t.Errorf("reason = %q, want %q", blocked.Reason.Key, ReasonRunning)
		}

		// …и в выбор ничего не попало
		if s.Contains(redis) || s.Len() != 0 {
			t.Errorf("redis got into the selection")
		}
	})

	t.Run("selecting the same item twice counts it once", func(t *testing.T) {
		// Given: DerivedData уже отмечена
		s := NewSelection()
		mustAdd(t, s, derivedData)

		// When: отмечаем её ещё раз (двойной клик по галочке)
		mustAdd(t, s, derivedData)

		// Then: размер не удвоился
		if s.Len() != 1 || s.Total() != derivedData.Size {
			t.Errorf("Len() = %d, Total() = %s, want 1 and %s", s.Len(), s.Total(), derivedData.Size)
		}
	})

	t.Run("unselecting removes the item from the total", func(t *testing.T) {
		// Given: отмечены два объекта
		s := NewSelection()
		mustAdd(t, s, derivedData)
		mustAdd(t, s, npmCache)

		// When: снимаем отметку с одного
		s.Remove(derivedData)

		// Then: остался только второй
		if s.Contains(derivedData) || s.Total() != npmCache.Size {
			t.Errorf("Total() = %s, want %s", s.Total(), npmCache.Size)
		}
	})

	t.Run("same ID in different categories are different items", func(t *testing.T) {
		// Given: образ и контейнер Docker с одинаковым ID
		image := Item{ID: "abc", CategoryID: "docker.images", Name: "postgres:17", Size: 300}
		container := Item{ID: "abc", CategoryID: "docker.containers", Name: "old-postgres", Size: 200}
		s := NewSelection()

		// When: отмечаем оба
		mustAdd(t, s, image)
		mustAdd(t, s, container)

		// Then: это два разных объекта
		if s.Len() != 2 || s.Total() != 500 {
			t.Errorf("Len() = %d, Total() = %s, want 2 and 500 B", s.Len(), s.Total())
		}
	})

	t.Run("items come back in a stable order", func(t *testing.T) {
		// Given: отмечены объекты в одном порядке
		s := NewSelection()
		mustAdd(t, s, npmCache)
		mustAdd(t, s, derivedData)

		// When: просим список несколько раз
		first := s.Items()
		second := s.Items()

		// Then: порядок одинаковый (по категории, потом по ID)
		if len(first) != 2 || first[0].Name != "npm cache" || second[0].Name != first[0].Name {
			t.Errorf("Items() order = %v / %v", first, second)
		}
	})

	t.Run("clear removes everything", func(t *testing.T) {
		// Given: что-то отмечено
		s := NewSelection()
		mustAdd(t, s, derivedData)

		// When: снимаем все отметки
		s.Clear()

		// Then: выбор пуст
		if s.Len() != 0 {
			t.Errorf("Len() = %d after Clear, want 0", s.Len())
		}
	})
}
