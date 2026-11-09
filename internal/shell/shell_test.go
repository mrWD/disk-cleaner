package shell

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	ctx := context.Background()

	t.Run("arguments are passed as is, without a shell", func(t *testing.T) {
		// When: передаём аргумент с ; и $ — в оболочке это были бы спецсимволы
		out, err := Run(ctx, "echo", "a; rm -rf $HOME")

		// Then: echo просто напечатал строку, ничего не выполнилось
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(out)) != "a; rm -rf $HOME" {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("failed command explains why", func(t *testing.T) {
		// When: ls несуществующей папки
		_, err := Run(ctx, "ls", "/definitely/not/here")

		// Then: в ошибке есть текст из stderr
		if err == nil || !strings.Contains(err.Error(), "No such file") {
			t.Errorf("err = %v, want stderr in the message", err)
		}
	})

	t.Run("missing program is reported as not found", func(t *testing.T) {
		_, err := Run(ctx, "no-such-program-xyz")

		if !errors.Is(err, exec.ErrNotFound) {
			t.Errorf("err = %v, want exec.ErrNotFound", err)
		}
	})
}

func TestEnvHasFullPath(t *testing.T) {
	// Then: в PATH для запускаемых программ есть папка Homebrew,
	// даже если приложение запустили из Finder с коротким PATH.
	t.Setenv("PATH", "/usr/bin:/bin") // как у приложения из Finder

	for _, kv := range Env() {
		if strings.HasPrefix(kv, "PATH=") && strings.Contains(kv, "/opt/homebrew/bin") {
			return
		}
	}
	t.Error("PATH in Env() has no /opt/homebrew/bin")
}
