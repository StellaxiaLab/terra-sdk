package modulert

import (
	"encoding/json"
	"testing"
)

func TestCoreInvocationRoundTrip(t *testing.T) {
	original := CoreInvocation{OperationID: "terra.storage.snapshot.create", VersionRange: ">=1.0.0 <2.0.0", Input: json.RawMessage(`{"path":"/x"}`), TimeoutMS: 5000}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round CoreInvocation
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.OperationID != original.OperationID || round.VersionRange != original.VersionRange || round.TimeoutMS != 5000 {
		t.Fatalf("round-trip mismatch: %#v", round)
	}
}
