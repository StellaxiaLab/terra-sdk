package procguard

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// TestMain doubles as the program the tests launch: the test binary run with
// PROCGUARD_HELPER set sleeps or allocates instead of running tests. Only
// processes this test starts are ever touched.
func TestMain(m *testing.M) {
	switch os.Getenv("PROCGUARD_HELPER") {
	case "sleep":
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case "spin":
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
		}
		os.Exit(0)
	case "alloc":
		mb, _ := strconv.Atoi(os.Getenv("PROCGUARD_MB"))
		buf := make([]byte, mb<<20)
		for i := 0; i < len(buf); i += 4096 {
			buf[i] = 1
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(t *testing.T, mode string, extra ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=NoSuchTest")
	cmd.Env = append(os.Environ(), "PROCGUARD_HELPER="+mode)
	cmd.Env = append(cmd.Env, extra...)
	Prepare(cmd)
	return cmd
}

func TestLimitsIsZero(t *testing.T) {
	if !(Limits{}).IsZero() {
		t.Fatal("zero Limits is not zero")
	}
	if (Limits{CPUMillicores: 1}).IsZero() || (Limits{MemoryBytes: 1}).IsZero() {
		t.Fatal("a Limits with a ceiling reads as zero")
	}
}

func TestElevatedAnswersForThisProcess(t *testing.T) {
	elevated, err := Elevated()
	if err != nil && !elevated {
		t.Fatalf("an unreadable answer must read as elevated, got false with %v", err)
	}
	t.Logf("elevated=%v err=%v", elevated, err)
}

func TestTerminateStopsTheProgram(t *testing.T) {
	cmd := helper(t, "sleep")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c, err := Contain(cmd, Limits{})
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatal(err)
	}
	defer c.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	c.Terminate(cmd)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the program outlived Terminate")
	}
}

func TestUsageMeasuresTheProgram(t *testing.T) {
	cmd := helper(t, "spin")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c, err := Contain(cmd, Limits{})
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatal(err)
	}
	defer c.Close()
	time.Sleep(150 * time.Millisecond)
	usage, err := c.Usage()
	if errors.Is(err, ErrUsageUnavailable) {
		t.Skip("no usage measurement on " + runtime.GOOS)
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	t.Logf("usage=%+v", usage)
	if usage.CPUTime <= 0 && usage.MemoryBytes == 0 && usage.PeakMemoryBytes == 0 {
		t.Fatalf("usage measured nothing: %+v", usage)
	}
}

// Asking for a ceiling either gets it or gets ErrLimitEnforcementUnavailable;
// the program never runs unlimited behind a nil error.
func TestLimitsAreEnforcedOrReportedUnavailable(t *testing.T) {
	cmd := helper(t, "sleep")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	c, err := Contain(cmd, Limits{CPUMillicores: 500, MemoryBytes: 512 << 20})
	if err != nil {
		if !errors.Is(err, ErrLimitEnforcementUnavailable) {
			t.Fatalf("a refused ceiling must wrap ErrLimitEnforcementUnavailable: %v", err)
		}
		t.Logf("ceiling unavailable here: %v", err)
		return
	}
	defer c.Close()
	c.Terminate(cmd)
}
