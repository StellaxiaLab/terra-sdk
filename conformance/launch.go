package conformance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Spec says how to start the module under test.
type Spec struct {
	// Command and Args start the module. It is run as a child process whose
	// environment is the current one plus the six contract variables.
	Command string
	Args    []string
	Dir     string
	// ModuleID is the id the module is told it represents.
	// Default: io.terra.conformance.sample.
	ModuleID string
	// OperationPath is passed to Check (optional).
	OperationPath string
	// BindTimeout is how long the module has to start listening. Default 20s.
	BindTimeout time.Duration
	// Launches is how many times the module is started in a row, each with a
	// fresh identity (the contract forbids caching identity or port across
	// launches). Default 2.
	Launches int
}

func (s *Spec) defaults() {
	if s.ModuleID == "" {
		s.ModuleID = "io.terra.conformance.sample"
	}
	if s.BindTimeout <= 0 {
		s.BindTimeout = 20 * time.Second
	}
	if s.Launches <= 0 {
		s.Launches = 2
	}
}

// Run launches the module Spec.Launches times, each with a fresh identity, and
// checks the contract on every launch. It returns an error only when the
// harness itself fails (bad command, no identity); contract violations are in
// the Report.
func Run(ctx context.Context, spec Spec) (Report, error) {
	spec.defaults()
	if spec.Command == "" {
		return Report{}, errors.New("conformance: Spec.Command is required")
	}
	var report Report
	for launch := 1; launch <= spec.Launches; launch++ {
		sub, err := runOnce(ctx, spec, uint64(launch))
		if err != nil {
			return report, fmt.Errorf("launch %d: %w", launch, err)
		}
		for _, result := range sub.Results {
			result.Name = fmt.Sprintf("[launch %d] %s", launch, result.Name)
			report.Results = append(report.Results, result)
		}
	}
	return report, nil
}

func runOnce(ctx context.Context, spec Spec, epoch uint64) (Report, error) {
	id, err := NewIdentity(spec.ModuleID, epoch)
	if err != nil {
		return Report{}, err
	}
	proc, err := start(ctx, spec, id)
	if err != nil {
		return Report{}, err
	}
	// Only the process this function started is ever stopped.
	defer proc.stop()

	if err := proc.waitBound(ctx, id.Endpoint, spec.BindTimeout); err != nil {
		var report Report
		report.add("module binds TERRA_LOOPBACK_ENDPOINT", err)
		return report, nil
	}
	return Check(ctx, id, CheckOptions{OperationPath: spec.OperationPath}), nil
}

type process struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
	output *syncBuffer
}

func start(ctx context.Context, spec Spec, id Identity) (*process, error) {
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), id.Env()...)
	out := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start %s: %w", spec.Command, err)
	}
	p := &process{cmd: cmd, cancel: cancel, done: make(chan struct{}), output: out}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *process) stop() {
	p.cancel()
	<-p.done
}

// waitBound polls until something accepts connections on endpoint, the module
// exits, or the timeout runs out.
func (p *process) waitBound(ctx context.Context, endpoint string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		conn, err := net.DialTimeout("tcp", endpoint, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-p.done:
			return fmt.Errorf("module exited before binding %s; output:\n%s", endpoint, p.output.String())
		case <-deadline.C:
			return fmt.Errorf("module did not bind %s within %s; output:\n%s", endpoint, timeout, p.output.String())
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() < 64<<10 {
		b.buf.Write(p)
	}
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
