package modulert

// State is a module lifecycle state (common design §8.1/§8.2, Scene·Player §8).
// A module is discovered, validated, dependency-resolved, activated and made
// ready; it may degrade or fail, and drains to stopped on shutdown.
type State string

const (
	StateDiscovered State = "discovered"
	StateValidated  State = "validated"
	StateResolved   State = "resolved"
	StateActivating State = "activating"
	StateReady      State = "ready"
	StateDegraded   State = "degraded"
	StateFailed     State = "failed"
	StateDraining   State = "draining"
	StateStopped    State = "stopped"
)

// validTransitions is the allowed state graph. `ready` means the module can
// serve its base capability, not merely that a process exists. `failed` may
// roll back to `resolved` (re-activate the last-known-good set) or stop.
var validTransitions = map[State]map[State]bool{
	StateDiscovered: {StateValidated: true, StateFailed: true},
	StateValidated:  {StateResolved: true, StateFailed: true},
	StateResolved:   {StateActivating: true, StateFailed: true},
	StateActivating: {StateReady: true, StateDegraded: true, StateFailed: true},
	StateReady:      {StateDegraded: true, StateDraining: true, StateFailed: true},
	StateDegraded:   {StateReady: true, StateDraining: true, StateFailed: true},
	StateDraining:   {StateStopped: true, StateFailed: true},
	StateFailed:     {StateResolved: true, StateStopped: true},
	StateStopped:    {StateDiscovered: true},
}

// KnownState reports whether s is a defined lifecycle state.
func KnownState(s State) bool {
	_, ok := validTransitions[s]
	return ok
}

// CanTransition reports whether from→to is an allowed lifecycle transition.
func CanTransition(from, to State) bool {
	return validTransitions[from][to]
}

// IsTerminalStable reports whether s is a settled operating state (ready or
// stopped) rather than an in-flight or failure state.
func IsTerminalStable(s State) bool {
	return s == StateReady || s == StateStopped
}

// NextStates returns the states reachable from s in a deterministic-free order
// (callers that need ordering should sort). It is primarily for diagnostics.
func NextStates(from State) []State {
	transitions := validTransitions[from]
	states := make([]State, 0, len(transitions))
	for state := range transitions {
		states = append(states, state)
	}
	return states
}
