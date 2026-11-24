//go:build windows

package procguard

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

// On Windows the ceilings are Job Object limits and always available, so they
// are held to a real program.
func TestWindowsMemoryCeilingStopsAnAllocatingProgram(t *testing.T) {
	cmd := helper(t, "alloc", "PROCGUARD_MB=400")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c, err := Contain(cmd, Limits{MemoryBytes: 96 << 20})
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatal(err)
	}
	defer c.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if err == nil || !errors.As(err, &exit) {
			t.Fatalf("a program past its memory ceiling should be stopped, got %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the memory ceiling did not stop the program")
	}
}

func TestWindowsCPUCeilingIsAccepted(t *testing.T) {
	cmd := helper(t, "sleep")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c, err := Contain(cmd, Limits{CPUMillicores: 250})
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatal(err)
	}
	defer c.Close()
	c.Terminate(cmd)
	_, _ = cmd.Process.Wait()
}
