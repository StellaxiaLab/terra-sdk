package protocol

import "testing"

// The three cases that matter are the two near-misses, not the hit: an
// exclusion built on one condition instead of two would drop real hardware or
// another project's virtual device.
func TestIsTerraVirtualInput(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		vendor  uint16
		product uint16
		sysfs   string
		want    bool
	}{
		{
			// The device the 2026-09-03 uinput probe created on marui-server.
			name: "our projection", vendor: VirtualInputVendor, product: VirtualInputProduct,
			sysfs: "/devices/virtual/input/input11", want: true,
		},
		{
			// A pid.codes device someone plugged in. Real hardware, real bus path.
			name: "same identity on a real bus", vendor: VirtualInputVendor, product: VirtualInputProduct,
			sysfs: "/devices/pci0000:00/0000:00:14.0/usb1/1-3/1-3:1.0/0003:1209:7E88.0007/input/input9",
			want:  false,
		},
		{
			// Another project's virtual device — a portal shim, a loopback.
			name: "someone else's virtual device", vendor: 0x0000, product: 0x0000,
			sysfs: "/devices/virtual/input/input12", want: false,
		},
		{
			name: "physical mouse", vendor: 0x046d, product: 0xc548,
			sysfs: "/devices/pci0000:00/0000:00:14.0/usb1/1-1/input/input3", want: false,
		},
	} {
		if got := IsTerraVirtualInput(testCase.vendor, testCase.product, testCase.sysfs); got != testCase.want {
			t.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
}
