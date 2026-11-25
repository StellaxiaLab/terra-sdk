//go:build linux

package procguard

import "syscall"

func setParentDeathSignal(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGKILL }
