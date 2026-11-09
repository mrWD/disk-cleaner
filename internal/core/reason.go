package core

// Reason — причина, почему объект нельзя трогать.
//
// Ядро не хранит готовый текст на каком-то языке: только ключ перевода
// и значения для подстановки. Текст на нужном языке соберёт UI.
// Так ядро не зависит от языка интерфейса.
type Reason struct {
	Key  string         // ключ перевода, например "simulators.shared_image"
	Args map[string]any // подстановки в текст: {"Runtime": "iOS 27.0"}
}

// Ключи причин, которые знает само ядро. Их переводы лежат
// в переводах интерфейса (internal/ui/translations).
// const — значения, известные ещё при компиляции. Менять их нельзя.
const (
	ReasonRunning = "core.reason.running"
	ReasonInUse   = "core.reason.in_use"
)
