package core

// Reason is why an item must not be touched.
//
// The core doesn't store ready-made text in any language: only a translation
// key and the values to substitute. The UI assembles the text in the right language.
// This way the core doesn't depend on the interface language.
type Reason struct {
	Key  string         // translation key, for example "simulators.shared_image"
	Args map[string]any // values substituted into the text: {"Runtime": "iOS 27.0"}
}

// Reason keys that the core itself knows. Their translations live
// in the interface translations (internal/ui/translations).
// const values are known at compile time. They can't be changed.
const (
	ReasonRunning = "core.reason.running"
	ReasonInUse   = "core.reason.in_use"
)
