package core

// UnavailableError means the provider can't search right now: the program it
// needs isn't installed or isn't running (for example, Docker is off).
//
// This is not a failure of our program, it is a normal situation. The UI shows
// the reason as calm text, not as a red error.
type UnavailableError struct {
	Reason Reason
}

func (e *UnavailableError) Error() string {
	return "unavailable: " + e.Reason.Key
}

// Unavailable is a short way to return such an error from Scan:
//
//	return nil, core.Unavailable("docker.not_running")
func Unavailable(key string) error {
	return &UnavailableError{Reason: Reason{Key: key}}
}
