package core

import (
	"strconv"
	"strings"
)

// Action — одно конкретное действие очистки.
// Заполнено ровно одно из двух: RemovePath или Command.
type Action struct {
	RemovePath string   // удалить этот путь целиком
	Command    []string // или выполнить команду: {"docker", "image", "rm", "abc"}
	NeedsAdmin bool     // нужен пароль администратора

	// Trash — не удалять RemovePath навсегда, а переместить в Корзину.
	// Так поступаем с файлами пользователя (например, из «Загрузок»):
	// ошибку можно исправить, вернув файл из Корзины.
	Trash bool
}

// String показывает действие так, как оно выглядело бы в терминале.
// Это только для окна подтверждения: Executor выполняет команды
// напрямую, без shell, поэтому кавычки и спецсимволы там не опасны.
func (a Action) String() string {
	var parts []string // nil-срез: append сам выделит память

	if a.NeedsAdmin {
		parts = append(parts, "sudo")
	}

	switch {
	case a.RemovePath != "" && a.Trash:
		parts = append(parts, "trash", shellQuote(a.RemovePath)) // /usr/bin/trash в macOS
	case a.RemovePath != "":
		parts = append(parts, "rm", "-rf", shellQuote(a.RemovePath))
	default:
		// range по срезу: _ — индекс, arg — значение.
		for _, arg := range a.Command {
			parts = append(parts, shellQuote(arg))
		}
	}

	return strings.Join(parts, " ")
}

// shellQuote берёт строку в кавычки, только если без них
// терминал понял бы её неправильно (пробелы, $, * и т.п.).
func shellQuote(s string) string {
	const special = " \t\n\"'\\$`!*?&;|<>(){}[]#~"
	if s != "" && !strings.ContainsAny(s, special) {
		return s
	}
	return strconv.Quote(s) // "строка в кавычках" с экранированием
}
