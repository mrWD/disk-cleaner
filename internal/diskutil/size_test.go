package diskutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"disk-cleaner/internal/core"
)

// writeFile создаёт файл нужного размера. t.Helper — чтобы ошибка
// показывала строку сценария, а не строку внутри помощника.
func writeFile(t *testing.T, path string, size int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// make([]byte, size) — срез из size нулевых байт.
	// Нули пишем через 1, чтобы APFS не сделал файл «разреженным».
	data := make([]byte, size)
	for i := range data {
		data[i] = 1
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

const mb = 1_000_000

func TestDirSize(t *testing.T) {
	ctx := context.Background() // пустой контекст: без отмены и таймаута

	t.Run("folder size is the sum of its files on disk", func(t *testing.T) {
		// Given: папка с двумя файлами по 1 MB (t.TempDir сам удалится после теста)
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.bin"), mb)
		writeFile(t, filepath.Join(dir, "sub", "b.bin"), mb)

		// When: считаем размер
		size, err := DirSize(ctx, dir)

		// Then: примерно 2 MB (на диске чуть больше из-за блоков)
		if err != nil {
			t.Fatal(err)
		}
		if size < 2*mb || size > 2*mb+64_000 {
			t.Errorf("DirSize = %s, want about 2.0 MB", size)
		}
	})

	t.Run("hard links are counted once", func(t *testing.T) {
		// Given: файл 1 MB и жёсткая ссылка на него (так делает pnpm)
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.bin"), mb)
		if err := os.Link(filepath.Join(dir, "a.bin"), filepath.Join(dir, "link.bin")); err != nil {
			t.Fatal(err)
		}

		// When: считаем размер
		size, _ := DirSize(ctx, dir)

		// Then: 1 MB, а не 2
		if size > mb+64_000 {
			t.Errorf("DirSize = %s, want about 1.0 MB", size)
		}
	})

	t.Run("symbolic links are not followed", func(t *testing.T) {
		// Given: большая папка снаружи и ссылка на неё внутри нашей
		outside := t.TempDir()
		writeFile(t, filepath.Join(outside, "big.bin"), 3*mb)
		dir := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}

		// When: считаем размер нашей папки
		size, _ := DirSize(ctx, dir)

		// Then: чужие 3 MB не посчитались
		if size >= mb {
			t.Errorf("DirSize = %s, the symlink target was counted", size)
		}
	})

	t.Run("missing path is an error", func(t *testing.T) {
		_, err := DirSize(ctx, filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Error("want an error for a missing path")
		}
	})

	t.Run("cancelled scan stops", func(t *testing.T) {
		// Given: папка с большим числом файлов и уже отменённый контекст
		dir := t.TempDir()
		for i := range 5000 { // range по числу: i = 0…4999
			writeFile(t, filepath.Join(dir, fmt.Sprintf("f%04d.bin", i)), 1)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		// When: считаем размер
		_, err := DirSize(cancelled, dir)

		// Then: получили ошибку отмены
		if err == nil {
			t.Error("want context.Canceled, got nil")
		}
	})
}

func TestSizeAll(t *testing.T) {
	t.Run("sizes come back in the same order as paths", func(t *testing.T) {
		// Given: две папки разного размера и одна несуществующая
		small := t.TempDir()
		writeFile(t, filepath.Join(small, "a.bin"), mb)
		big := t.TempDir()
		writeFile(t, filepath.Join(big, "a.bin"), 3*mb)
		missing := filepath.Join(t.TempDir(), "nope")

		// When: считаем параллельно
		sizes := SizeAll(context.Background(), []string{big, missing, small}, 2)

		// Then: порядок сохранился, у несуществующей — 0
		if sizes[0] < 3*mb || sizes[1] != 0 || sizes[2] < mb || sizes[2] >= 2*mb {
			t.Errorf("SizeAll = %v", []core.Bytes(sizes))
		}
	})
}
