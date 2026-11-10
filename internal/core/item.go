package core

// Status is what is currently happening to an item.
// It is a separate type based on int, so that you can't
// pass an arbitrary number in place of a status by accident.
type Status int

// iota is a counter inside a const block: 0, 1, 2…
// The order matters: every type in Go has a "zero value"
// (for int it is 0). So an Item without an explicit Status is StatusIdle.
const (
	StatusIdle    Status = iota // 0: sitting idle, can be deleted
	StatusRunning               // 1: running right now (container, simulator)
	StatusInUse                 // 2: needed by something else (an image needed by a container)
)

// Item is one concrete thing that was found: an image, a folder, a simulator.
type Item struct {
	ID         string // unique within the category: a path, an image ID, a UUID
	CategoryID string // which category it belongs to, for example "docker.images"
	Name       string // what to show: "postgres:17", "iOS 18.4"
	Detail     string // details: path, tag, date
	Size       Bytes  // how much space will be freed
	Status     Status

	// Busy says who exactly is running or using the item: "used by emulator
	// Lingary_Test" is clearer than just "in use". Optional:
	// if nil, the generic reason for the status is shown.
	Busy *Reason

	// Protected is protection from deletion. nil = not protected.
	// * means a pointer: "a reference to a Reason, or nothing (nil)".
	// A plain Reason can't be "nothing", but a pointer can.
	Protected *Reason
}

// CanSelect is a core rule: can the item be marked for deletion.
// It returns two values: yes/no, and the reason if no.
// The UI doesn't duplicate this rule, it only asks.
func (it Item) CanSelect() (bool, *Reason) {
	// Protection beats status: its reason is more specific
	// ("shared with iOS 27.0" is more useful than just "running").
	if it.Protected != nil {
		return false, it.Protected
	}

	// An idle item can be selected, nothing more to check.
	if it.Status == StatusIdle {
		return true, nil
	}

	// It's busy: if the provider knows who exactly is using it, say so.
	if it.Busy != nil {
		return false, it.Busy
	}

	// switch in Go doesn't fall through to the next case,
	// so no break is needed.
	switch it.Status {
	case StatusRunning:
		return false, &Reason{Key: ReasonRunning} // & takes the address, giving a pointer
	default:
		return false, &Reason{Key: ReasonInUse}
	}
}
