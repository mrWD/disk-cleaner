package core

import (
	"fmt"
	"maps"
	"slices"
)

// BlockedError — ошибка «этот объект нельзя выбрать». Хранит, какой
// объект и почему. В Go ошибка — это любой тип с методом Error() string:
// тип error — это интерфейс ровно с одним таким методом.
type BlockedError struct {
	Item   Item
	Reason Reason
}

// Error делает BlockedError ошибкой. Текст — для логов и разработчика;
// пользователю UI покажет перевод по Reason.Key.
func (e *BlockedError) Error() string {
	return fmt.Sprintf("cannot select %q: %s", e.Item.Name, e.Reason.Key)
}

// Selection — то, что пользователь отметил для удаления.
//
// Это агрегат (термин DDD): поле items с маленькой буквы, поэтому снаружи
// пакета core его не видно и напрямую в него ничего не положить.
// Менять выбор можно только методами ниже, а они следят за правилами.
type Selection struct {
	items map[string]Item // ключ — keyOf(item), значение — сам объект
}

// NewSelection создаёт пустой выбор.
// map нужно создать явно: у map нулевое значение nil,
// и запись в nil-map роняет программу.
func NewSelection() *Selection {
	return &Selection{items: map[string]Item{}}
}

// keyOf — уникальный ключ объекта во всём приложении.
// ID уникален только внутри категории (у образа и контейнера Docker
// могут совпасть ID), поэтому добавляем категорию.
// Маленькая буква: вспомогательная функция только для этого пакета.
func keyOf(it Item) string {
	return it.CategoryID + "/" + it.ID
}

// Add отмечает объект. Если его нельзя выбрать — возвращает *BlockedError
// и ничего не меняет. Повторное добавление того же объекта ничего не меняет.
//
// (s *Selection) — получатель-указатель: метод меняет сам Selection,
// а не его копию.
func (s *Selection) Add(it Item) error {
	// if с коротким объявлением: ok и why видны только внутри этого if.
	if ok, why := it.CanSelect(); !ok {
		return &BlockedError{Item: it, Reason: *why} // *why — значение по указателю
	}

	s.items[keyOf(it)] = it

	return nil
}

// Remove снимает отметку. Если объекта не было — ничего не происходит.
func (s *Selection) Remove(it Item) {
	delete(s.items, keyOf(it)) // встроенная функция удаления из map
}

// Contains — отмечен ли объект.
func (s *Selection) Contains(it Item) bool {
	// Чтение из map возвращает два значения: само значение и «нашлось ли».
	// Значение нам не нужно, поэтому _.
	_, found := s.items[keyOf(it)]
	return found
}

// Len — сколько объектов отмечено.
func (s *Selection) Len() int {
	return len(s.items)
}

// Total — сколько места освободится.
func (s *Selection) Total() Bytes {
	var total Bytes // нулевое значение — 0

	for _, it := range s.items {
		total += it.Size
	}

	return total
}

// Clear снимает все отметки.
func (s *Selection) Clear() {
	clear(s.items) // встроенная функция: удаляет из map все записи
}

// Items — все отмеченные объекты в стабильном порядке.
// Порядок обхода map в Go специально случайный, поэтому сортируем ключи:
// иначе план удаления выглядел бы по-разному при каждом открытии.
func (s *Selection) Items() []Item {
	keys := slices.Sorted(maps.Keys(s.items)) // ключи map → отсортированный срез

	// make создаёт пустой срез нужной вместимости, чтобы append
	// не перевыделял память на каждом шаге.
	out := make([]Item, 0, len(keys))
	for _, k := range keys {
		out = append(out, s.items[k])
	}

	return out
}

// ItemsFor — отмеченные объекты, которые принадлежат провайдеру p:
// те, чья категория есть в p.Categories(). Так ядру не нужно
// угадывать провайдера по началу строки CategoryID.
func (s *Selection) ItemsFor(p Provider) []Item {
	// map[string]bool как множество: «есть ли такая категория у провайдера».
	own := map[string]bool{}
	for _, c := range p.Categories() {
		own[c.ID] = true
	}

	var out []Item
	for _, it := range s.Items() {
		if own[it.CategoryID] {
			out = append(out, it)
		}
	}

	return out
}
