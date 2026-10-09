package protocol

import "strings"

// Terra-created virtual input devices carry a reserved identity.
//
// On Linux a uinput device is indistinguishable from a physical one: it
// appears in /proc/bus/input/devices with the same fields, and the only thing
// separating it from real hardware is its sysfs path. That was measured — a
// probe device classified as an ordinary mouse through the same code path
// io.terra.io-inventory uses to enumerate hardware.
//
// Left alone that closes a loop. A virtual mouse created on node B to
// reproduce node A's mouse is enumerated on node B as one of node B's own
// devices, published again as node B's SVI resource, and offered to node C —
// which would then be subscribing to B's copy of A's mouse. Each hop adds
// latency and an owner, and revoking A's grant would not reach C.
//
// So the creating side stamps a reserved bus/vendor/product identity, and the
// enumerating side skips exactly that identity when it also sits under the
// kernel's virtual devices. Two conditions rather than one: a physical device
// that happened to share the vendor/product pair is not under
// /devices/virtual, and a virtual device created by other software (a
// v4l2loopback node, a Wayland portal shim) does not carry our pair, so
// neither is silently dropped.
//
// This lives here rather than in either module because both sides must agree:
// the module that creates the device and the module that must not re-publish
// it are different modules, and a constant copied into both drifts.
const (
	// VirtualInputBus is BUS_USB. A virtual device has no real bus; USB is the
	// value udev's rules understand best, and the vendor/product pair is what
	// carries the identity.
	VirtualInputBus uint16 = 0x0003
	// VirtualInputVendor is pid.codes (0x1209), the vendor id reserved for
	// open-source projects. Terra does not hold a USB vendor id and must not
	// squat on someone else's.
	VirtualInputVendor uint16 = 0x1209
	// VirtualInputProduct identifies io-weave's projected input devices within
	// that vendor space.
	VirtualInputProduct uint16 = 0x7e88
)

// virtualSysfsPrefix is where the kernel puts devices with no physical parent,
// uinput's among them.
const virtualSysfsPrefix = "/devices/virtual/"

// IsTerraVirtualInput reports whether an enumerated input device is one Terra
// itself created — a projection of another node's device, not hardware
// attached here.
//
// Both the reserved identity and the virtual sysfs path must hold. A caller
// that knows only one of them must not guess the other: excluding on the path
// alone would drop legitimate virtual devices, and excluding on the identity
// alone would drop real hardware that shares a pid.codes product id.
func IsTerraVirtualInput(vendor, product uint16, sysfsPath string) bool {
	if vendor != VirtualInputVendor || product != VirtualInputProduct {
		return false
	}
	return strings.HasPrefix(sysfsPath, virtualSysfsPrefix)
}
