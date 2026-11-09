package core

// Status — что сейчас происходит с объектом.
// Это отдельный тип на основе int, чтобы нельзя было
// случайно передать вместо статуса любое число.
type Status int

// iota — счётчик внутри блока const: 0, 1, 2…
// Порядок важен: у любого типа в Go есть «нулевое значение»
// (у int это 0). Значит, Item без явного Status будет StatusIdle.
const (
	StatusIdle    Status = iota // 0: лежит без дела, можно удалять
	StatusRunning               // 1: запущен прямо сейчас (контейнер, симулятор)
	StatusInUse                 // 2: нужен чему-то другому (образ нужен контейнеру)
)

// Item — один конкретный найденный объект: образ, папка, симулятор.
type Item struct {
	ID         string // уникален внутри категории: путь, ID образа, UUID
	CategoryID string // к какой категории относится, например "docker.images"
	Name       string // что показать: "postgres:17", "iOS 18.4"
	Detail     string // подробности: путь, тег, дата
	Size       Bytes  // сколько места освободится
	Status     Status

	// Busy — кем именно запущен или занят объект: "used by emulator
	// Lingary_Test" понятнее, чем просто "in use". Необязательно:
	// если nil, показывается общая причина для статуса.
	Busy *Reason

	// Protected — защита от удаления. nil = защиты нет.
	// * значит указатель: «ссылка на Reason или ничего (nil)».
	// Обычный Reason не умеет быть «ничем», а указатель умеет.
	Protected *Reason
}

// CanSelect — правило ядра: можно ли отметить объект для удаления.
// Возвращает два значения: да/нет и причину, если нет.
// UI это правило не дублирует, а только спрашивает.
func (it Item) CanSelect() (bool, *Reason) {
	// Защита важнее статуса: её причина конкретнее
	// ("shared with iOS 27.0" полезнее, чем просто "running").
	if it.Protected != nil {
		return false, it.Protected
	}

	// Свободный объект можно выбрать — дальше проверять нечего.
	if it.Status == StatusIdle {
		return true, nil
	}

	// Занят: если провайдер знает, кем именно, — говорим это.
	if it.Busy != nil {
		return false, it.Busy
	}

	// switch в Go не проваливается в следующий case,
	// поэтому break не нужен.
	switch it.Status {
	case StatusRunning:
		return false, &Reason{Key: ReasonRunning} // & — взять адрес, получить указатель
	default:
		return false, &Reason{Key: ReasonInUse}
	}
}
