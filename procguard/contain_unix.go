//go:build !windows

package procguard

import (
	"os/exec"
	"syscall"
)

// Prepare readies cmd before Start: each program leads its own process group
// so a stop reaches its children, and on Linux it is killed if its parent dies
// (FR-H5).
func Prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setParentDeathSignal(cmd.SysProcAttr)
}

// Containment is a started program and, when a ceiling was asked for, the
// cgroup that enforces it.
type Containment struct {
	pid int
	cg  *cgroup
}

// Contain wraps the started cmd. With zero limits it only tracks the process
// group. With limits it needs cgroup v2 write access (Linux); when that is not
// there the error wraps ErrLimitEnforcementUnavailable and the caller must kill
// cmd instead of running it without the ceiling.
func Contain(cmd *exec.Cmd, limits Limits) (*Containment, error) {
	c := &Containment{pid: cmd.Process.Pid}
	if limits.IsZero() {
		return c, nil
	}
	cg, err := enforce(cmd.Process.Pid, limits)
	if err != nil {
		return nil, err
	}
	c.cg = cg
	return c, nil
}

// Interrupt asks the program's whole group to stop. It reports whether the
// signal was delivered.
func (c *Containment) Interrupt(cmd *exec.Cmd) bool {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) == nil
}

// Terminate kills the program's whole group.
func (c *Containment) Terminate(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
	if c != nil {
		c.cg.killAll()
	}
}

// Close releases the containment (removes the cgroup, if one was made).
func (c *Containment) Close() {
	if c != nil {
		c.cg.remove()
	}
}

// Usage reports what the program has used: from its cgroup when it has one,
// otherwise from the process itself (Linux only).
func (c *Containment) Usage() (Usage, error) {
	if c == nil {
		return Usage{}, ErrUsageUnavailable
	}
	if c.cg != nil {
		return c.cg.usage()
	}
	return processUsage(c.pid)
}
