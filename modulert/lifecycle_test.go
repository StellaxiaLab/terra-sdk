package modulert

import "testing"

func TestCanTransitionHappyPath(t *testing.T) {
	path := []State{StateDiscovered, StateValidated, StateResolved, StateActivating, StateReady, StateDraining, StateStopped}
	for index := 0; index+1 < len(path); index++ {
		if !CanTransition(path[index], path[index+1]) {
			t.Fatalf("expected legal transition %s→%s", path[index], path[index+1])
		}
	}
}

func TestCanTransitionRejectsIllegal(t *testing.T) {
	illegal := [][2]State{
		{StateDiscovered, StateReady}, // must be validated/resolved first
		{StateReady, StateActivating}, // no going back to activating
		{StateStopped, StateReady},    // stopped only rescans to discovered
		{StateReady, StateReady},      // no self-loop
	}
	for _, pair := range illegal {
		if CanTransition(pair[0], pair[1]) {
			t.Fatalf("did not expect legal transition %s→%s", pair[0], pair[1])
		}
	}
}

func TestFailureRollbackAndDegradedRecovery(t *testing.T) {
	if !CanTransition(StateActivating, StateFailed) {
		t.Fatalf("activating→failed expected")
	}
	if !CanTransition(StateFailed, StateResolved) {
		t.Fatalf("failed→resolved rollback expected")
	}
	if !CanTransition(StateDegraded, StateReady) {
		t.Fatalf("degraded→ready recovery expected")
	}
}

func TestStateClassifiers(t *testing.T) {
	if !IsTerminalStable(StateReady) || !IsTerminalStable(StateStopped) {
		t.Fatalf("ready and stopped should be stable")
	}
	if IsTerminalStable(StateActivating) {
		t.Fatalf("activating is not a stable state")
	}
	if !KnownState(StateReady) || KnownState(State("bogus")) {
		t.Fatalf("KnownState classification wrong")
	}
	if len(NextStates(StateActivating)) != 3 {
		t.Fatalf("activating should reach 3 states, got %v", NextStates(StateActivating))
	}
}
