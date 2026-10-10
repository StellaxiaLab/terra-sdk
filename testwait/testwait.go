// Package testwait is the one place a Go test in a Terra module says "wait
// until this is true".
//
// It is the wait helper Terra's own tests use, published here so a module that
// builds without a Terra checkout keeps the same budgets instead of growing a
// private copy of the same three lines. The code is copied from Terra's
// terra-testwait unchanged; only this comment differs.
//
// # What it does not do
//
// It does not make a test *assert* a duration. Every use of this package is a
// wait — the timeout branch is always a failure, never a measurement — so
// raising the budget is safe here. A test that claims "this finishes within
// 200ms" is a performance assertion: it must not move when a runner is busy, so
// it keeps its own constant and says so in a comment. Do not route it through
// this package.
//
// # The scale knob
//
// A busy runner does not need different code, it needs more time.
// TERRA_TEST_WAIT_SCALE multiplies every budget, so a loaded machine is a CI
// setting rather than a patch across many files.
//
// The poll interval deliberately does NOT scale. Scaling it would make a test
// answer later on the machine that is already slow, which is backwards.
package testwait

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

// DefaultBudget is how long a wait gets before it is a failure.
//
// 5s is not a new number: of the eight helpers that hard-coded a timeout, four
// already used it. Picking the mode rather than inventing a value means most
// call sites keep the behaviour they had.
const DefaultBudget = 5 * time.Second

// PollInterval is how often a condition is re-checked. 10ms is the middle of
// what the replaced helpers used (5ms · 10ms · 200ms).
const PollInterval = 10 * time.Millisecond

// ScaleEnvVar multiplies every budget. Set it on the runner, not in code.
const ScaleEnvVar = "TERRA_TEST_WAIT_SCALE"

var (
	scaleOnce  sync.Once
	scaleValue float64
)

// scale reads TERRA_TEST_WAIT_SCALE once.
//
// A malformed value panics rather than falling back to 1. A typo in CI
// configuration that silently runs at 1x is the worst outcome available here:
// the runner stays as flaky as it was and the setting that was supposed to fix
// it reads as applied. Failing loudly costs one obvious build error.
func scale() float64 {
	scaleOnce.Do(func() {
		parsed, err := parseScale(os.LookupEnv(ScaleEnvVar))
		if err != nil {
			panic(err.Error())
		}
		scaleValue = parsed
	})
	return scaleValue
}

// parseScale is where the rule lives, separate from the caching so it can be
// tested. A knob whose parsing cannot be tested is a knob that breaks quietly.
func parseScale(raw string, present bool) (float64, error) {
	if !present || raw == "" {
		return 1, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s=%q is not a positive number", ScaleEnvVar, raw)
	}
	return parsed, nil
}

// Budget is the wait budget in force for this run — DefaultBudget times the
// scale. Tests print it; they do not need to compute it.
func Budget() time.Duration {
	return scaled(DefaultBudget)
}

func scaled(budget time.Duration) time.Duration {
	return scaleBudget(budget, scale())
}

// scaleBudget takes the factor as an argument so it can be tested at a factor
// other than 1. Reading the cached scale() inside would make every test of this
// run at 1x, where "multiply by the factor" and "ignore the factor" produce the
// same answer — a test that cannot fail.
func scaleBudget(budget time.Duration, factor float64) time.Duration {
	if factor == 1 {
		return budget
	}
	return time.Duration(float64(budget) * factor)
}

// Scale applies the run's multiplier to a duration the caller owns.
//
// For the waits this package cannot swallow whole: a `case <-time.After(...)`
// inside a select, or a hand-rolled deadline loop whose body is too particular
// to fold. Those keep their shape and their own number; Scale is what makes the
// number answer to the same knob as everything else, so a loaded runner does not
// have to be handled twice.
//
// It is NOT for a duration a test asserts on. Scaling a performance claim makes
// it pass on a slow machine, which is the opposite of what it is for.
func Scale(budget time.Duration) time.Duration {
	return scaled(budget)
}

// Until waits for cond to hold, and fails the test if the budget runs out.
//
// what completes the sentence "timed out waiting for ..." — write the state
// being waited on ("the module to report ready"), not the action that should
// cause it. The message is read by whoever sees the failure, not by whoever
// wrote the wait.
func Until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	UntilFor(t, DefaultBudget, what, cond)
}

// UntilFor is Until with a budget this one site needs to differ on. The budget
// is scaled like every other, so a site that needs longer stays proportional to
// the rest when a runner is loaded.
//
// Reach for it only with a reason worth a comment: a wait that crosses a
// process boundary, spawns a build, or touches a real socket. "It was flaky"
// is not that reason — that is what the scale knob is for.
func UntilFor(t *testing.T, budget time.Duration, what string, cond func() bool) {
	t.Helper()
	started := time.Now()
	if BecameWithin(budget, cond) {
		return
	}
	t.Fatal(timeoutMessage(what, time.Since(started), scaled(budget), scale()))
}

// timeoutMessage is split out for the same reason as parseScale: this string is
// the entire diagnostic a reader gets, and the only way to check that it names
// the budget AND the scale is to build it somewhere a test can call.
//
// Naming both matters. A 5s budget running at scale 4 fails after 20s, and a
// message that says only one of those numbers sends the reader looking for a
// bug in the wrong place.
func timeoutMessage(what string, elapsed, budget time.Duration, factor float64) string {
	return fmt.Sprintf("timed out waiting for %s after %s (budget %s, %s=%g)",
		what, elapsed.Round(time.Millisecond), budget, ScaleEnvVar, factor)
}

// Became reports whether cond held within the budget, without failing the test.
//
// For the caller that has something to do either way — "if it came up, assert
// the happy path; if not, assert the fallback". A caller that only wants the
// test to stop wants Until instead: this one hands back a bool, and a bool that
// nobody checks is a wait that silently did nothing.
func Became(cond func() bool) bool {
	return BecameWithin(DefaultBudget, cond)
}

// BecameWithin is Became with a per-site budget.
func BecameWithin(budget time.Duration, cond func() bool) bool {
	// Check before sleeping: a condition that is already true must not cost a
	// poll interval, because many of these run inside loops over fixtures.
	if cond() {
		return true
	}
	// A zero or negative budget means "check once" rather than "wait forever" —
	// the opposite reading turns a mistake into a hang.
	deadline := time.Now().Add(scaled(budget))
	for time.Now().Before(deadline) {
		time.Sleep(PollInterval)
		if cond() {
			return true
		}
	}
	return false
}

// Context returns a context carrying the wait budget, cancelled when the test
// ends.
//
// This is the seam for the other shape the sites take: not a polling loop but a
// budget handed to a call that blocks. Both get their duration from the same
// place, which is the point.
func Context(t *testing.T) context.Context {
	t.Helper()
	return ContextFor(t, DefaultBudget)
}

// ContextFor is Context with a per-site budget.
func ContextFor(t *testing.T, budget time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), scaled(budget))
	t.Cleanup(cancel)
	return ctx
}
