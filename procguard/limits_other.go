//go:build !windows && !linux

package procguard

import "fmt"

type cgroup struct{}

func enforce(pid int, limits Limits) (*cgroup, error) {
	return nil, fmt.Errorf("%w: no CPU or memory ceiling on this platform", ErrLimitEnforcementUnavailable)
}

func (g *cgroup) killAll()              {}
func (g *cgroup) remove()               {}
func (g *cgroup) usage() (Usage, error) { return Usage{}, ErrUsageUnavailable }
func processUsage(int) (Usage, error)   { return Usage{}, ErrUsageUnavailable }
