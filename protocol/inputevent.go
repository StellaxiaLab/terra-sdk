package protocol

// The wire format for projected input.
//
// Two modules must agree on this: the one that reads a device and the one that
// reproduces it on another node. They are different modules on different
// machines with different displays and different keyboard layouts, so what
// travels between them cannot be either side's local units.
//
// Two measurements (2026-09-03, docs/ideas/io-weave-virtual-io-ideas.md §8.8)
// fixed the two hard choices here.

// PointerPosition is a point in normalized device space: 0 is the left or top
// edge, PointerPositionMax the right or bottom one, on whatever display the
// receiving node has.
//
// Relative deltas were measured and rejected. On Windows the same injected
// dx=8 moved 6, 8, 8 and 8 pixels in one run, and dx=1 sometimes moved
// nothing at all — pointer acceleration sits in the path, so a delta is not a
// distance. An edge hop is "put the cursor there", not "move it that much",
// and only an absolute position survives the trip between two machines with
// different acceleration curves and different display sizes. Sending deltas
// would leave each node's cursor somewhere slightly different, and the error
// would accumulate for as long as the session lasted.
//
// The range matches the space Windows' MOUSEEVENTF_ABSOLUTE already uses, so
// the commonest sink converts with a multiply rather than a policy.
type PointerPosition struct {
	X uint16 `json:"x"`
	Y uint16 `json:"y"`
}

// PointerPositionMax is the inclusive upper bound of normalized device space.
const PointerPositionMax = 65535

// ToPixels converts a normalized position to a pixel on a display of this
// size. It is the sink's half of the contract, kept here so both ends round
// the same way.
func (p PointerPosition) ToPixels(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	// width rather than width-1: this is the mapping Windows itself applies to
	// absolute injection, measured as 32768 landing on 1280 of 2560.
	return int(p.X) * width / (PointerPositionMax + 1), int(p.Y) * height / (PointerPositionMax + 1)
}

// NormalizePixels is the source's half — a pixel on a display of this size
// becomes a position any sink can place.
func NormalizePixels(x, y, width, height int) PointerPosition {
	clamp := func(value, size int) uint16 {
		if size <= 1 {
			return 0
		}
		if value < 0 {
			value = 0
		}
		if value > size-1 {
			value = size - 1
		}
		return uint16(value * PointerPositionMax / (size - 1))
	}
	return PointerPosition{X: clamp(x, width), Y: clamp(y, height)}
}

// PointerButton names the buttons a projected pointer can carry. Names rather
// than numbers: the OS button numbering differs between platforms, and a
// number on the wire would silently mean different buttons at each end.
type PointerButton string

const (
	PointerButtonLeft   PointerButton = "left"
	PointerButtonRight  PointerButton = "right"
	PointerButtonMiddle PointerButton = "middle"
)

// PointerEvent is one thing a pointer did.
type PointerEvent struct {
	// Position is where the pointer is. Always present: a button press
	// without a position cannot be replayed on a display of another size.
	Position PointerPosition `json:"position"`
	// Buttons holds the buttons held down at this moment — the full set, not a
	// change. A sink that misses one event must still end up in the right
	// state, and a fleet where mouse events may be dropped (QoS
	// realtime-latest) cannot rebuild state from deltas.
	Buttons []PointerButton `json:"buttons,omitempty"`
	// ScrollX and ScrollY are in detents, the unit both platforms report.
	//
	// Positive ScrollY is a wheel turned away from the person — the direction
	// that shows earlier content. Positive ScrollX is a tilt to the right.
	// Spelled out because the sign is the one thing two implementations will
	// disagree about while both believing they are correct, and an inverted
	// wheel is not a crash, just a thing that feels wrong forever.
	ScrollX int32 `json:"scroll_x,omitempty"`
	ScrollY int32 `json:"scroll_y,omitempty"`
}

// KeyEvent is one key going down or up.
//
// It carries a key *code*, not a character. The alternative — sending the
// character the source produced — was rejected because it cannot express a
// held modifier, a key repeat, or a shortcut: Ctrl+C is not the character
// "c". The cost is that a code means different things under different
// layouts, which is what SourceLayout is for.
type KeyEvent struct {
	// Usage is the physical key as a USB HID usage id on the keyboard usage
	// page (0x07): 0x04 is the key engraved "A" on a US board, 0x1E the one
	// engraved "1".
	//
	// NOT the source OS's own numbering. Windows virtual-key codes and Linux
	// evdev codes are different spaces that overlap numerically — evdev 30 is
	// "a" and VK 30 is nothing in particular — so a code without a space is a
	// number that means two things. Rather than carry a discriminator and make
	// every sink learn both spaces, the source converts to the one identity
	// both platforms already map to internally.
	Usage uint16 `json:"usage"`
	// Down says whether this is a press or a release.
	Down bool `json:"down"`
	// Repeat marks an auto-repeat rather than a fresh press.
	Repeat bool `json:"repeat,omitempty"`
	// SourceLayout identifies the keyboard layout in force where the key was
	// pressed (a BCP-47-ish tag such as "ko-KR" or an OS layout id).
	//
	// The sink translates with this rather than assuming its own layout. The
	// first implementation translates in Terra (L2) because whether the OS can
	// be told to hold a per-device layout (L1) is still unmeasured — no node
	// in the fleet runs a graphical session, so the spike could not run. The
	// field is what lets L1 replace L2 later without a wire change: an L1 sink
	// uses it to set the layout on the virtual device instead of remapping
	// each code.
	SourceLayout string `json:"source_layout,omitempty"`
}
