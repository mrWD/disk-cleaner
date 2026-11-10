package diskutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewestFile(t *testing.T) {
	// Given: an old file and a newer one deeper down
	dir := t.TempDir()
	old, fresh := filepath.Join(dir, "old.txt"), filepath.Join(dir, "a", "b", "new.txt")
	for _, p := range []string{old, fresh} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldTime, newTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), time.Date(2026, 10, 25, 11, 0, 0, 0, time.UTC)
	_ = os.Chtimes(old, oldTime, oldTime)
	_ = os.Chtimes(fresh, newTime, newTime)

	// Then: the newer one's time, wherever it is
	if got := NewestFile(context.Background(), dir); !got.Equal(newTime) {
		t.Errorf("NewestFile = %v, want %v", got, newTime)
	}
	if got := NewestFile(context.Background(), filepath.Join(dir, "missing")); !got.IsZero() {
		t.Errorf("missing folder: %v, want zero", got)
	}
}
