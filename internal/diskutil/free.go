// Package diskutil — работа с диском: свободное место, размер папок.
// Это инфраструктура: зависит от core, но не наоборот.
package diskutil

import (
	"syscall"

	"disk-cleaner/internal/core" // свои пакеты: имя модуля + путь к папке
)

// FreeSpace возвращает свободное и общее место на диске, где лежит path.
// Функция в Go может вернуть несколько значений. Последнее по традиции — ошибка.
//
// Имя с большой буквы = функция видна из других пакетов (экспортируется).
// С маленькой — только внутри diskutil. В Go это вместо public/private.
func FreeSpace(path string) (core.Bytes, core.Bytes, error) {
	// Пустая структура, которую заполнит система.
	var st syscall.Statfs_t

	// &st — адрес структуры: Statfs записывает результат прямо в неё.
	err := syscall.Statfs(path, &st)
	if err != nil {
		// Ошибки в Go не выбрасываются, а возвращаются. Проверяем сразу.
		return 0, 0, err
	}

	// Диск поделён на блоки одного размера.
	// Bavail — свободные блоки, доступные обычному пользователю,
	// Blocks — всего блоков, Bsize — размер блока в байтах.
	// Типы у полей разные (uint64 и uint32), а Go сам числа не преобразует,
	// поэтому приводим всё к int64 явно.
	blockSize := int64(st.Bsize)
	free := core.Bytes(int64(st.Bavail) * blockSize)
	total := core.Bytes(int64(st.Blocks) * blockSize)

	return free, total, nil // nil — «ошибки нет»
}
