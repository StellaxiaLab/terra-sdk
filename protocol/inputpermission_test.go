package protocol

import "testing"

func TestInputTierForKind(t *testing.T) {
	for kind, want := range map[string]InputPermissionTier{
		"mouse":    InputTierPointer,
		"keyboard": InputTierKeyboard,
		"raw-bus":  InputTierRaw,
		"camera":   InputTierObserve,
		"":         InputTierObserve,
		// A kind nobody has mapped yet. It must not pick up injection rights
		// by resembling something.
		"gamepad": InputTierObserve,
	} {
		if got := InputTierForKind(kind); got != want {
			t.Errorf("%q -> %q, want %q", kind, got, want)
		}
	}
}

// The tiers must stay distinct values. A refactor that collapsed two of them
// would silently widen every grant that named the survivor.
func TestTiersDoNotCollapse(t *testing.T) {
	seen := map[InputPermissionTier]bool{}
	for _, tier := range []InputPermissionTier{InputTierObserve, InputTierPointer, InputTierKeyboard, InputTierRaw} {
		if tier == "" {
			t.Error("a tier is empty, which would make an unset field look granted")
		}
		if seen[tier] {
			t.Errorf("duplicate tier %q", tier)
		}
		seen[tier] = true
	}
}
