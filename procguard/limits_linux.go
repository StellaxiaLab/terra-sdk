//go:build linux

package procguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A ceiling on Linux is a cgroup v2 directory under the cgroup this process is
// already in, with cpu.max and memory.max written and the program moved in.
// That only works where the owner may write there (a delegated cgroup, such as
// a systemd service with Delegate=yes, or root). Anywhere else the answer is
// ErrLimitEnforcementUnavailable: a ceiling that cannot be set is reported, not
// faked.
//
// The program joins the cgroup after it started, so anything it forked in that
// instant stays outside; the program itself and what it starts later are held.

const cgroupRoot = "/sys/fs/cgroup"

type cgroup struct{ dir string }

func enforce(pid int, limits Limits) (*cgroup, error) {
	unavailable := func(why error) (*cgroup, error) {
		return nil, fmt.Errorf("%w: %v", ErrLimitEnforcementUnavailable, why)
	}
	own, err := ownCgroup()
	if err != nil {
		return unavailable(err)
	}
	dir := filepath.Join(cgroupRoot, own, "terra-procguard-"+strconv.Itoa(os.Getpid())+"-"+strconv.Itoa(pid))
	if err := os.Mkdir(dir, 0o755); err != nil {
		return unavailable(err)
	}
	fail := func(why error) (*cgroup, error) {
		_ = os.Remove(dir)
		return unavailable(why)
	}
	if limits.CPUMillicores > 0 {
		// cpu.max is "<quota> <period>" in microseconds: 1000 millicores is one
		// period of quota per period.
		const period = 100000
		quota := limits.CPUMillicores * period / 1000
		if err := os.WriteFile(filepath.Join(dir, "cpu.max"), []byte(fmt.Sprintf("%d %d", quota, period)), 0o644); err != nil {
			return fail(err)
		}
	}
	if limits.MemoryBytes > 0 {
		if err := os.WriteFile(filepath.Join(dir, "memory.max"), []byte(strconv.FormatUint(limits.MemoryBytes, 10)), 0o644); err != nil {
			return fail(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return fail(err)
	}
	return &cgroup{dir: dir}, nil
}

func ownCgroup() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "0::"); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("no cgroup v2 entry in /proc/self/cgroup")
}

func (g *cgroup) killAll() {
	if g != nil {
		_ = os.WriteFile(filepath.Join(g.dir, "cgroup.kill"), []byte("1"), 0o644)
	}
}

func (g *cgroup) remove() {
	if g == nil {
		return
	}
	g.killAll()
	for i := 0; i < 20; i++ {
		if err := os.Remove(g.dir); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (g *cgroup) usage() (Usage, error) {
	var u Usage
	stat, err := os.ReadFile(filepath.Join(g.dir, "cpu.stat"))
	if err != nil {
		return Usage{}, err
	}
	for _, line := range strings.Split(string(stat), "\n") {
		if v, ok := strings.CutPrefix(line, "usage_usec "); ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				u.CPUTime = time.Duration(n) * time.Microsecond
			}
		}
	}
	u.MemoryBytes = readUint(filepath.Join(g.dir, "memory.current"))
	u.PeakMemoryBytes = readUint(filepath.Join(g.dir, "memory.peak")) // kernel 5.19+, else 0
	return u, nil
}

func readUint(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return n
}

// processUsage measures one process from /proc (its children are not summed).
func processUsage(pid int) (Usage, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return Usage{}, err
	}
	// After the ")" that closes the command name, state is field 3, so utime
	// and stime (fields 14 and 15) are index 11 and 12 of what follows.
	text := string(data)
	i := strings.LastIndexByte(text, ')')
	if i < 0 {
		return Usage{}, ErrUsageUnavailable
	}
	fields := strings.Fields(text[i+1:])
	if len(fields) < 13 {
		return Usage{}, ErrUsageUnavailable
	}
	const clockTick = 100 // USER_HZ is 100 on every Linux Go supports
	utime, _ := strconv.ParseInt(fields[11], 10, 64)
	stime, _ := strconv.ParseInt(fields[12], 10, 64)
	u := Usage{CPUTime: time.Duration(utime+stime) * time.Second / clockTick}
	if statm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm"); err == nil {
		if f := strings.Fields(string(statm)); len(f) >= 2 {
			pages, _ := strconv.ParseUint(f[1], 10, 64)
			u.MemoryBytes = pages * uint64(os.Getpagesize())
		}
	}
	return u, nil
}
