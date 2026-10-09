package protocol

// Permission tiers for projected input.
//
// Permission is granted by tier rather than by device class because a class is
// not a capability. A `raw` endpoint passes arbitrary HID reports, so a device
// that presents a keyboard descriptor can inject keys through a grant that
// only ever named "raw" — the class said one thing and the traffic did
// another.
//
// The rule that makes tiers work is that they do NOT nest: holding `keyboard`
// does not imply `pointer`, and `raw` implies neither. An approval meant to
// share a mouse must not quietly also open typing, and typing is command
// execution.
type InputPermissionTier string

const (
	// InputTierObserve reads only — a device list, a printer's status.
	InputTierObserve InputPermissionTier = "observe"
	// InputTierPointer injects pointer motion and buttons.
	InputTierPointer InputPermissionTier = "pointer"
	// InputTierKeyboard injects keys. Treat it as remote command execution,
	// because that is what typing into a focused terminal is.
	InputTierKeyboard InputPermissionTier = "keyboard"
	// InputTierRaw impersonates an arbitrary device. A sink holding this must
	// still parse the descriptor and require every tier the usages imply.
	InputTierRaw InputPermissionTier = "raw"
)

// InputTierForKind returns the tier an SVI io kind's injection needs, given
// the kind token as it appears in an `io.*` resource kind ("mouse",
// "keyboard", "raw-bus", …).
//
// Anything unrecognised gets observe. That direction is deliberate: an unknown
// device should be readable and not injectable, and a new kind that nobody
// mapped must not inherit injection rights by accident.
func InputTierForKind(kind string) InputPermissionTier {
	switch kind {
	case "mouse", "pointer":
		return InputTierPointer
	case "keyboard":
		return InputTierKeyboard
	case "raw-bus", "raw":
		return InputTierRaw
	default:
		return InputTierObserve
	}
}
