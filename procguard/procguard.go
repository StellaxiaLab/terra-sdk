// Package procguard is the one place that answers four questions about a
// program a Terra component launches: is this process elevated, can the
// program be held to a CPU and memory ceiling, does it die with its parent, and
// how much did it use.
//
// The code moved here from the workload module (contain_windows.go,
// contain_unix.go, pdeath_linux.go, elevation_*.go) so the Derra carry-over
// (a Job's ceiling) and later isolation do not grow a second copy. What the
// workload module did is unchanged: every launched program is held by a
// containment that kills it when the owner goes away (FR-H5), and a failure to
// read the elevation answer counts as elevated. What is new is Limits and
// Usage.
//
// Typical use:
//
//	cmd := exec.Command(path, args...)
//	procguard.Prepare(cmd)
//	if err := cmd.Start(); err != nil { ... }
//	c, err := procguard.Contain(cmd, procguard.Limits{CPUMillicores: 2000, MemoryBytes: 4 << 30})
//	if err != nil { _ = cmd.Process.Kill(); ... } // never run uncontained
//	defer c.Close()
//
// Limits that cannot be enforced are an error (ErrLimitEnforcementUnavailable),
// never a silent run without the ceiling (ADR-DR-001 D4).
package procguard

import (
	"errors"
	"time"
)

// ErrLimitEnforcementUnavailable means the requested ceiling cannot be
// enforced on this machine. The caller reports LIMIT_ENFORCEMENT_UNAVAILABLE
// and does not run the program.
var ErrLimitEnforcementUnavailable = errors.New("procguard: limit enforcement unavailable")

// ErrUsageUnavailable means this platform cannot measure the program's use.
var ErrUsageUnavailable = errors.New("procguard: usage measurement unavailable")

// Limits is the ceiling placed on a contained program. A zero field means no
// limit on that resource; the zero Limits only contains (kill on close).
type Limits struct {
	// CPUMillicores is the most CPU the whole program may use, in thousandths
	// of a core: 2000 is two cores. It is a rate cap, not a CPU-time budget.
	CPUMillicores int
	// MemoryBytes is the most memory the whole program may hold.
	MemoryBytes uint64
}

// IsZero reports whether no ceiling is asked for.
func (l Limits) IsZero() bool { return l.CPUMillicores <= 0 && l.MemoryBytes == 0 }

// Usage is what the contained program has used so far. A field the platform
// cannot measure is zero.
type Usage struct {
	// CPUTime is the processor time spent (user + kernel).
	CPUTime time.Duration
	// MemoryBytes is the memory held now.
	MemoryBytes uint64
	// PeakMemoryBytes is the most memory held so far.
	PeakMemoryBytes uint64
}
