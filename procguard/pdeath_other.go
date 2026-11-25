//go:build !linux && !windows

package procguard

import "syscall"

func setParentDeathSignal(a *syscall.SysProcAttr) {}
