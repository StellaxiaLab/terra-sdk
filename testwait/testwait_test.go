package testwait

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestBecameReturnsImmediatelyWhenAlreadyTrue(t *testing.T) {
	calls := 0
	started := time.Now()
	ok := Became(func() bool { calls++; return true })
	if !ok {
		t.Fatal("a condition that is already true must report true")
	}
	// Checking before the first sleep is the whole point: these run inside
	// loops over fixtures, and paying a poll interval per already-true check
	// turns a fast suite into a slow one.
	if calls != 1 {
		t.Fatalf("condition checked %d times, want exactly 1", calls)
	}
	if elapsed := time.Since(started); elapsed > PollInterval {
		t.Fatalf("waited %s for a condition that was already true", elapsed)
	}
}

func TestBecameWithinPollsUntilTrue(t *testing.T) {
	calls := 0
	ok := BecameWithin(time.Second, func() bool { calls++; return calls >= 3 })
	if !ok {
		t.Fatal("a condition that becomes true inside the budget must report true")
	}
	if calls != 3 {
		t.Fatalf("condition checked %d times, want 3", calls)
	}
}

func TestBecameWithinGivesUpAtTheBudget(t *testing.T) {
	started := time.Now()
	if BecameWithin(50*time.Millisecond, func() bool { return false }) {
		t.Fatal("a condition that never holds must report false")
	}
	elapsed := time.Since(started)
	if elapsed < 50*time.Millisecond {
		t.Fatalf("gave up after %s, before the 50ms budget", elapsed)
	}
	// Generous upper bound: this asserts the loop ends, not how fast.
	if elapsed > 2*time.Second {
		t.Fatalf("took %s to give up on a 50ms budget", elapsed)
	}
}

func TestZeroBudgetChecksOnceRatherThanWaitingForever(t *testing.T) {
	// The opposite reading — zero means no deadline — turns a mistyped budget
	// into a hang, which is the failure mode this whole module exists to end.
	calls := 0
	done := make(chan bool, 1)
	go func() { done <- BecameWithin(0, func() bool { calls++; return false }) }()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a false condition must report false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a zero budget hung instead of checking once")
	}
	if calls != 1 {
		t.Fatalf("condition checked %d times, want exactly 1", calls)
	}
}

func TestNegativeBudgetAlsoChecksOnce(t *testing.T) {
	done := make(chan bool, 1)
	go func() { done <- BecameWithin(-time.Minute, func() bool { return true }) }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("a true condition must report true even with a negative budget")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a negative budget hung")
	}
}

func TestUntilPassesWhenTheConditionHolds(t *testing.T) {
	calls := 0
	Until(t, "the condition to hold", func() bool { calls++; return calls >= 2 })
	if calls < 2 {
		t.Fatalf("condition checked %d times, want at least 2", calls)
	}
}

func TestContextCarriesTheBudgetAndIsCancelledWithTheTest(t *testing.T) {
	ctx := ContextFor(t, 80*time.Millisecond)
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("the context must carry a deadline — that is what it is for")
	}
	if remaining := time.Until(deadline); remaining > time.Second {
		t.Fatalf("deadline is %s out, far past the 80ms budget", remaining)
	}
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("ended with %v, want a deadline", ctx.Err())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the context never ended")
	}
}

func TestParseScale(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		raw     string
		present bool
		want    float64
		wantErr bool
	}{
		{name: "unset is 1", present: false, want: 1},
		{name: "empty is 1", raw: "", present: true, want: 1},
		{name: "integer", raw: "4", present: true, want: 4},
		{name: "fraction", raw: "1.5", present: true, want: 1.5},
		// A typo that silently runs at 1x leaves the runner as flaky as it was
		// while the setting reads as applied. That is the worst outcome here.
		{name: "not a number", raw: "yes", present: true, wantErr: true},
		{name: "zero", raw: "0", present: true, wantErr: true},
		{name: "negative", raw: "-2", present: true, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := parseScale(testCase.raw, testCase.present)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("parseScale(%q) accepted a value it must reject", testCase.raw)
				}
				if !strings.Contains(err.Error(), ScaleEnvVar) {
					t.Fatalf("error %q does not name %s, so nobody knows what to fix", err, ScaleEnvVar)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseScale(%q) failed: %v", testCase.raw, err)
			}
			if got != testCase.want {
				t.Fatalf("parseScale(%q) = %v, want %v", testCase.raw, got, testCase.want)
			}
		})
	}
}

func TestTimeoutMessageNamesBothTheBudgetAndTheScale(t *testing.T) {
	// A 5s budget at scale 4 fails after 20s. A message carrying only one of
	// those numbers sends the reader hunting in the wrong place.
	message := timeoutMessage("the module to report ready", 20*time.Second, 20*time.Second, 4)
	for _, want := range []string{"the module to report ready", "20s", ScaleEnvVar, "4"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q is missing %q", message, want)
		}
	}
}

func TestScaleBudgetMultiplies(t *testing.T) {
	// Written the obvious way first — scaled(1s) vs 1s*scale() — this test could
	// not fail: the default scale is 1, so "multiply" and "ignore" agree. The
	// factor is an argument now so the test can pick one where they disagree.
	for _, testCase := range []struct {
		factor float64
		budget time.Duration
		want   time.Duration
	}{
		{factor: 1, budget: 5 * time.Second, want: 5 * time.Second},
		{factor: 4, budget: 5 * time.Second, want: 20 * time.Second},
		{factor: 0.5, budget: time.Second, want: 500 * time.Millisecond},
	} {
		if got := scaleBudget(testCase.budget, testCase.factor); got != testCase.want {
			t.Fatalf("scaleBudget(%s, %g) = %s, want %s",
				testCase.budget, testCase.factor, got, testCase.want)
		}
	}
}

func TestBudgetReflectsTheScaleInForce(t *testing.T) {
	if got, want := Budget(), scaleBudget(DefaultBudget, scale()); got != want {
		t.Fatalf("Budget() = %s, want %s", got, want)
	}
}

func TestPollIntervalDoesNotScale(t *testing.T) {
	// Scaling the interval would make a test answer *later* on the machine that
	// is already slow. Only the budget moves.
	if PollInterval != 10*time.Millisecond {
		t.Fatalf("PollInterval is %s; it is a constant by design", PollInterval)
	}
}

// The scale knob can only be tested at a factor other than 1, and scale() is
// read once per process — so these re-run the test binary with the environment
// set. Written the in-process way first, this suite passed while Scale ignored
// the multiplier entirely: at factor 1, "multiply" and "ignore" are the same
// answer. That is the third time in this work that a knob was checked where it
// could not be wrong.
const scaleProbeEnv = "TERRA_TESTWAIT_SCALE_PROBE"

func TestMain(m *testing.M) {
	if probe := os.Getenv(scaleProbeEnv); probe != "" {
		runScaleProbe(probe)
		return
	}
	os.Exit(m.Run())
}

// runScaleProbe prints what the package computed so the parent can check it.
// It runs instead of the suite, in a child process the parent started with a
// chosen TERRA_TEST_WAIT_SCALE.
func runScaleProbe(probe string) {
	switch probe {
	case "budget":
		fmt.Printf("budget=%s scale=%s one-second=%s\n", Budget(), time.Duration(float64(time.Second)*scale()), Scale(time.Second))
	case "panic":
		// Touching the knob is what triggers the parse; the panic is the point.
		_ = Budget()
		fmt.Println("no panic")
	}
	os.Exit(0)
}

func probeAtScale(t *testing.T, probe, factor string) string {
	t.Helper()
	command := exec.Command(os.Args[0])
	command.Env = append(os.Environ(), scaleProbeEnv+"="+probe, ScaleEnvVar+"="+factor)
	output, err := command.CombinedOutput()
	if probe == "panic" {
		return string(output) // the caller inspects both the failure and the text
	}
	if err != nil {
		t.Fatalf("probe at %s=%s failed: %v\n%s", ScaleEnvVar, factor, err, output)
	}
	return string(output)
}

func TestScaleAndBudgetBothFollowTheKnob(t *testing.T) {
	out := probeAtScale(t, "budget", "4")
	for _, want := range []string{"budget=20s", "scale=4s", "one-second=4s"} {
		if !strings.Contains(out, want) {
			t.Fatalf("at scale 4 the package reported %q, missing %q", strings.TrimSpace(out), want)
		}
	}
}

func TestScaleAgreesWithBudgetAtEveryFactor(t *testing.T) {
	// Scale exists so a site keeping its own number still answers to the same
	// knob. If the two drifted, one knob would mean two things.
	for factor, wantBudget := range map[string]string{"1": "budget=5s", "2": "budget=10s", "0.5": "budget=2.5s"} {
		if out := probeAtScale(t, "budget", factor); !strings.Contains(out, wantBudget) {
			t.Fatalf("at %s=%s the package reported %q, want %q", ScaleEnvVar, factor, strings.TrimSpace(out), wantBudget)
		}
	}
}

func TestABadKnobStopsTheRunRatherThanQuietlyMeaningOne(t *testing.T) {
	out := probeAtScale(t, "panic", "yess")
	if strings.Contains(out, "no panic") {
		t.Fatal("a malformed scale ran at 1x instead of failing — the runner stays flaky while the setting reads as applied")
	}
	if !strings.Contains(out, ScaleEnvVar) {
		t.Fatalf("the failure %q does not name %s, so nobody knows what to fix", strings.TrimSpace(out), ScaleEnvVar)
	}
}
