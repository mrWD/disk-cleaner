package core

import "testing"

func TestActionString(t *testing.T) {
	cases := []struct {
		scenario string
		given    Action
		want     string
	}{
		{
			scenario: "removing a folder is shown as rm -rf",
			given:    Action{RemovePath: "/Users/viktor/Library/Developer/Xcode/DerivedData"},
			want:     "rm -rf /Users/viktor/Library/Developer/Xcode/DerivedData",
		},
		{
			scenario: "path with spaces is quoted",
			given:    Action{RemovePath: "/Users/viktor/Library/Application Support/Code/Cache"},
			// `...` — «сырая» строка: внутри можно писать " без экранирования
			want: `rm -rf "/Users/viktor/Library/Application Support/Code/Cache"`,
		},
		{
			scenario: "command is shown as typed in a terminal",
			given:    Action{Command: []string{"xcrun", "simctl", "runtime", "delete", "62544524-0B04-4C6B-9B44-4C486AFF6027"}},
			want:     "xcrun simctl runtime delete 62544524-0B04-4C6B-9B44-4C486AFF6027",
		},
		{
			scenario: "moving to the Trash is shown as trash",
			given:    Action{RemovePath: "/Users/viktor/Downloads/Lingary-191.aab", Trash: true},
			want:     "trash /Users/viktor/Downloads/Lingary-191.aab",
		},
		{
			scenario: "admin action starts with sudo",
			given:    Action{RemovePath: "/Library/Developer/CoreSimulator/Caches/dyld", NeedsAdmin: true},
			want:     "sudo rm -rf /Library/Developer/CoreSimulator/Caches/dyld",
		},
	}

	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			// When: показываем действие пользователю
			got := c.given.String()

			// Then: строка ровно такая, как в терминале
			if got != c.want {
				t.Errorf("String() =\n  %s\nwant\n  %s", got, c.want)
			}
		})
	}
}
