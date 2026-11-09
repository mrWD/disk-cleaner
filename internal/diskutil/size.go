package diskutil

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"disk-cleaner/internal/core"
)

// DirSize — сколько места на диске занимает path (папка или файл).
//
// Что важно:
//   - Считаются реально занятые блоки диска (как у du), а не «логический»
//     размер файлов: именно столько место и освободится.
//   - Обход не уходит на другие файловые системы. Иначе, как у du
//     08.11.2026, в размер попадёт содержимое смонтированных образов
//     или подключённого iPhone (DeviceFS показывал фантомные 60 ГБ).
//   - Символические ссылки не раскрываются: ссылка — это сама ссылка.
//   - Жёсткие ссылки на один файл (их делает pnpm) считаются один раз.
//   - Папки, которые не удалось прочитать, пропускаются.
func DirSize(ctx context.Context, path string) (core.Bytes, error) {
	// Lstat, а не Stat: если path — ссылка, берём саму ссылку, не цель.
	rootInfo, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}

	// info.Sys() — «сырые» данные системы. .(*syscall.Stat_t) — проверка
	// типа: «если внутри Stat_t, дай его, ok = true».
	rootStat, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return core.Bytes(rootInfo.Size()), nil
	}
	device := rootStat.Dev // номер файловой системы, на которой лежит path

	// Ключ для учёта жёстких ссылок: файл однозначно задаётся
	// номером устройства и номером inode.
	type inode struct {
		dev int64
		ino uint64
	}
	seen := map[inode]bool{}

	var total int64
	visited := 0

	// WalkDir обходит дерево и для каждого элемента вызывает нашу функцию.
	walkErr := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Нет доступа к папке или файл исчез во время обхода:
			// пропускаем этот элемент, но обход продолжаем.
			return nil
		}

		// Раз в несколько тысяч файлов проверяем, не отменили ли сканирование.
		visited++
		if visited%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return err // любая ошибка из функции останавливает WalkDir
			}
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			total += info.Size()
			return nil
		}

		// Другая файловая система — туда не заходим.
		if st.Dev != device {
			if d.IsDir() {
				return fs.SkipDir // специальная «ошибка»: пропустить папку целиком
			}
			return nil
		}

		// Файл с несколькими жёсткими ссылками считаем только при первой встрече.
		if !d.IsDir() && st.Nlink > 1 {
			key := inode{dev: int64(st.Dev), ino: st.Ino}
			if seen[key] {
				return nil
			}
			seen[key] = true
		}

		// Blocks — сколько блоков по 512 байт занято на диске.
		total += st.Blocks * 512
		return nil
	})

	if walkErr != nil {
		return core.Bytes(total), walkErr
	}

	return core.Bytes(total), nil
}

// SizeAll считает размеры нескольких путей параллельно и возвращает их
// в том же порядке. Если путь не удалось посчитать — его размер 0.
//
// Как это устроено (горутины — лёгкие потоки Go):
//   - на каждый путь запускаем горутину (go func() {...}());
//   - канал sem размером workers работает как «турникет»: одновременно
//     считают не больше workers горутин, чтобы не завалить диск;
//   - WaitGroup ждёт, пока все горутины закончат.
func SizeAll(ctx context.Context, paths []string, workers int) []core.Bytes {
	if workers < 1 {
		workers = 1
	}

	sizes := make([]core.Bytes, len(paths))
	sem := make(chan struct{}, workers) // буферизованный канал на workers мест
	var wg sync.WaitGroup

	for i, p := range paths {
		wg.Add(1)

		go func() {
			defer wg.Done() // defer — выполнится при выходе из функции, даже при ошибке

			sem <- struct{}{}        // занять место (ждём, если все заняты)
			defer func() { <-sem }() // освободить место в конце

			size, err := DirSize(ctx, p)
			if err == nil {
				// Каждая горутина пишет только в свою ячейку sizes[i],
				// поэтому они не мешают друг другу.
				sizes[i] = size
			}
		}()
	}

	wg.Wait()
	return sizes
}
