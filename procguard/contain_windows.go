//go:build windows

package procguard

import (
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// Each launched program is put in its own Job Object that kills its members when
// the handle closes. That is FR-H5: if the owner takes the process down, the
// handle closes with it and the programs go with it. The same Job Object carries
// the CPU rate cap and the memory ceiling, and answers Usage.

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW    = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJob   = kernel32.NewProc("SetInformationJobObject")
	procQueryInformationJob = kernel32.NewProc("QueryInformationJobObject")
	procAssignProcessToJob  = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject  = kernel32.NewProc("TerminateJobObject")
)

const (
	jobObjectBasicAccountingInformation = 1
	jobObjectExtendedLimitInformation   = 9
	jobObjectCPURateControlInformation  = 15

	jobLimitJobMemory      = 0x0200
	jobLimitKillOnJobClose = 0x2000

	cpuRateControlEnable  = 0x1
	cpuRateControlHardCap = 0x4

	processSetQuota = 0x0100
)

type ioCounters struct{ a, b, c, d, e, f uint64 }

type jobBasicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobExtendedLimit struct {
	Basic                 jobBasicLimit
	IO                    ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type jobCPURateControl struct {
	ControlFlags uint32
	CPURate      uint32 // hundredths of a percent of all processors (hard cap)
}

type jobBasicAccounting struct {
	TotalUserTime, TotalKernelTime                     int64 // 100 ns units
	ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses                uint32
	ActiveProcesses, TotalTerminatedProcesses          uint32
}

// Prepare readies cmd before Start. Windows needs nothing.
func Prepare(cmd *exec.Cmd) {}

// Containment is a started program held in its own Job Object.
type Containment struct{ job syscall.Handle }

// Contain puts the started cmd into a new Job Object that kills it when the
// handle closes, with limits applied. If a ceiling cannot be set the error
// wraps ErrLimitEnforcementUnavailable and the caller must kill cmd.
func Contain(cmd *exec.Cmd, limits Limits) (*Containment, error) {
	h, _, err := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return nil, err
	}
	fail := func(err error) (*Containment, error) {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, err
	}
	info := jobExtendedLimit{}
	info.Basic.LimitFlags = jobLimitKillOnJobClose
	if limits.MemoryBytes > 0 {
		info.Basic.LimitFlags |= jobLimitJobMemory
		info.JobMemoryLimit = uintptr(limits.MemoryBytes)
	}
	r, _, err := procSetInformationJob.Call(h, jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r == 0 {
		if limits.MemoryBytes > 0 {
			err = fmt.Errorf("%w: memory ceiling: %v", ErrLimitEnforcementUnavailable, err)
		}
		return fail(err)
	}
	if limits.CPUMillicores > 0 {
		// The rate is a share of all processors: millicores / (1000 * cpus),
		// in hundredths of a percent, at least 1 and at most 10000.
		rate := uint64(limits.CPUMillicores) * 10000 / (1000 * uint64(runtime.NumCPU()))
		if rate < 1 {
			rate = 1
		}
		if rate > 10000 {
			rate = 10000
		}
		cpu := jobCPURateControl{ControlFlags: cpuRateControlEnable | cpuRateControlHardCap, CPURate: uint32(rate)}
		if r, _, err := procSetInformationJob.Call(h, jobObjectCPURateControlInformation,
			uintptr(unsafe.Pointer(&cpu)), unsafe.Sizeof(cpu)); r == 0 {
			return fail(fmt.Errorf("%w: cpu ceiling: %v", ErrLimitEnforcementUnavailable, err))
		}
	}
	ph, err := syscall.OpenProcess(processSetQuota|syscall.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fail(err)
	}
	defer syscall.CloseHandle(ph)
	if r, _, err := procAssignProcessToJob.Call(h, uintptr(ph)); r == 0 {
		return fail(err)
	}
	return &Containment{job: syscall.Handle(h)}, nil
}

// Interrupt asks the program to stop politely. Windows has no signal that
// reaches an arbitrary console-less program, so there is nothing to send: the
// caller then terminates, and the stop response says forced=true.
func (c *Containment) Interrupt(cmd *exec.Cmd) bool { return false }

// Terminate kills every program in the containment.
func (c *Containment) Terminate(cmd *exec.Cmd) {
	if c != nil {
		procTerminateJobObject.Call(uintptr(c.job), 1)
	}
	_ = cmd.Process.Kill()
}

// Close releases the containment; anything still running in it is killed.
func (c *Containment) Close() {
	if c != nil {
		syscall.CloseHandle(c.job)
	}
}

// Usage reports what the program (all members of the Job Object) has used.
// Windows has no per-job "memory now", so MemoryBytes is zero and
// PeakMemoryBytes carries the figure.
func (c *Containment) Usage() (Usage, error) {
	if c == nil {
		return Usage{}, ErrUsageUnavailable
	}
	var acct jobBasicAccounting
	if r, _, err := procQueryInformationJob.Call(uintptr(c.job), jobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&acct)), unsafe.Sizeof(acct), 0); r == 0 {
		return Usage{}, err
	}
	var ext jobExtendedLimit
	if r, _, err := procQueryInformationJob.Call(uintptr(c.job), jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&ext)), unsafe.Sizeof(ext), 0); r == 0 {
		return Usage{}, err
	}
	return Usage{
		CPUTime:         time.Duration(acct.TotalUserTime+acct.TotalKernelTime) * 100,
		PeakMemoryBytes: uint64(ext.PeakJobMemoryUsed),
	}, nil
}
