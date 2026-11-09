package core

// UnavailableError — провайдер сейчас не может искать: нужная программа
// не установлена или не запущена (например, Docker выключен).
//
// Это не сбой программы, а нормальная ситуация. UI покажет причину
// спокойным текстом, а не красной ошибкой.
type UnavailableError struct {
	Reason Reason
}

func (e *UnavailableError) Error() string {
	return "unavailable: " + e.Reason.Key
}

// Unavailable — короткий способ вернуть такую ошибку из Scan:
//
//	return nil, core.Unavailable("docker.not_running")
func Unavailable(key string) error {
	return &UnavailableError{Reason: Reason{Key: key}}
}
