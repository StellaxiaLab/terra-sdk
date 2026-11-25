//go:build !windows

package procguard

import "os"

// Elevated reports whether the current process runs as root.
func Elevated() (bool, error) { return os.Geteuid() == 0, nil }
