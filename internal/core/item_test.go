package core

import "testing"

// Сценарии в стиле BDD: Given (дано) → When (когда) → Then (тогда).
// Имя каждого сценария — фраза о поведении, её видно в go test -v.
func TestItemCanSelect(t *testing.T) {
	cases := []struct {
		scenario string
		given    Item   // Given: такой объект
		wantOK   bool   // Then: можно ли его выбрать
		wantWhy  string // Then: ключ причины, если нельзя ("" — причины нет)
	}{
		{
			scenario: "idle cache can be selected",
			given:    Item{Name: "DerivedData"}, // Status не указан = StatusIdle
			wantOK:   true,
		},
		{
			scenario: "running container cannot be selected",
			given:    Item{Name: "redis:8", Status: StatusRunning},
			wantWhy:  ReasonRunning,
		},
		{
			scenario: "image used by a container cannot be selected",
			given:    Item{Name: "postgres:17", Status: StatusInUse},
			wantWhy:  ReasonInUse,
		},
		{
			// Реальный случай 08.11.2026: три записи iOS 27.0 с одним образом.
			scenario: "runtime sharing an image with the active one is protected",
			given: Item{
				Name: "iOS 27.0 (unusable copy)",
				Protected: &Reason{
					Key:  "simulators.shared_image",
					Args: map[string]any{"Runtime": "iOS 27.0"},
				},
			},
			wantWhy: "simulators.shared_image",
		},
		{
			scenario: "busy item says who uses it",
			given: Item{
				Name:   "android-35 system image",
				Status: StatusInUse,
				Busy:   &Reason{Key: "android.used_by_emulator", Args: map[string]any{"Emulator": "Lingary_Test"}},
			},
			wantWhy: "android.used_by_emulator",
		},
		{
			scenario: "busy note does not block an idle item",
			given:    Item{Name: "old cache", Busy: &Reason{Key: "stale.note"}},
			wantOK:   true,
		},
		{
			scenario: "protection is reported before running status",
			given: Item{
				Name:      "travel-planner worktree",
				Status:    StatusRunning,
				Protected: &Reason{Key: "worktrees.unmerged_commits"},
			},
			wantWhy: "worktrees.unmerged_commits",
		},
	}

	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			// When: спрашиваем ядро, можно ли выбрать объект
			ok, why := c.given.CanSelect()

			// Then: ответ совпадает с ожидаемым.
			// Fatalf останавливает сценарий: если ok не тот, причину проверять бессмысленно.
			if ok != c.wantOK {
				t.Fatalf("CanSelect() ok = %v, want %v", ok, c.wantOK)
			}

			gotWhy := ""
			if why != nil {
				gotWhy = why.Key
			}
			// Errorf отмечает провал, но даёт сценарию дойти до конца.
			if gotWhy != c.wantWhy {
				t.Errorf("reason = %q, want %q", gotWhy, c.wantWhy)
			}
		})
	}
}
