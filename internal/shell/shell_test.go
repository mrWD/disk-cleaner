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
		// When: we pass an argument with ; and $, which would be special characters in a shell
		out, err := Run(ctx, "echo", "a; rm -rf $HOME")

		// Then: echo just printed the string, nothing was executed
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(out)) != "a; rm -rf $HOME" {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("failed command explains why", func(t *testing.T) {
		// When: ls on a folder that doesn't exist
		_, err := Run(ctx, "ls", "/definitely/not/here")

		// Then: the error contains the text from stderr
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
	// Then: the PATH for the programs we start includes the Homebrew folder,
	// even if the app was launched from Finder with a short PATH.
	t.Setenv("PATH", "/usr/bin:/bin") // like an app launched from Finder has

	for _, kv := range Env() {
		if strings.HasPrefix(kv, "PATH=") && strings.Contains(kv, "/opt/homebrew/bin") {
			return
		}
	}
	t.Error("PATH in Env() has no /opt/homebrew/bin")
}
