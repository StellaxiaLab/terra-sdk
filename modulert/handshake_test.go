package modulert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// moduleStub is an in-test module endpoint: it answers the Terra handshake and
// readiness probes, gated by the workload credential, the way terra-module-sdk
// will. echoNonce/echoInstance let a test force an identity mismatch.
type moduleStub struct {
	identity         WorkloadIdentity
	ready            bool
	detail           string
	contributionHash string
	echoInstance     string // overrides identity.InstanceID in the reply when set
	echoNonce        string // overrides identity.Nonce in the reply when set
}

func (m *moduleStub) handler() http.Handler {
	mux := http.NewServeMux()
	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(CredentialHeader) != m.identity.Credential {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc(HandshakePath, guard(func(w http.ResponseWriter, r *http.Request) {
		instance := m.identity.InstanceID
		if m.echoInstance != "" {
			instance = m.echoInstance
		}
		nonce := m.identity.Nonce
		if m.echoNonce != "" {
			nonce = m.echoNonce
		}
		_ = json.NewEncoder(w).Encode(HandshakeResponse{InstanceID: instance, Nonce: nonce, ContributionHash: m.contributionHash})
	}))
	mux.HandleFunc(ReadinessPath, guard(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ReadinessResponse{Ready: m.ready, Detail: m.detail})
	}))
	return mux
}

// serveStub starts the stub and returns an identity whose Endpoint points at it.
func serveStub(t *testing.T, stub *moduleStub) WorkloadIdentity {
	t.Helper()
	server := httptest.NewServer(stub.handler())
	t.Cleanup(server.Close)
	id := stub.identity
	id.Endpoint = strings.TrimPrefix(server.URL, "http://")
	return id
}

func readinessContract() *Readiness {
	return &Readiness{OperationID: "io.terra.backup.health.get", TimeoutMS: 2000}
}

func baseIdentity() WorkloadIdentity {
	return WorkloadIdentity{ModuleID: "io.terra.backup", InstanceID: "inst-1", Epoch: 3, Nonce: "nonce-1", Credential: "cred-1"}
}

func TestActivateHappyPathReachesReady(t *testing.T) {
	id := serveStub(t, &moduleStub{identity: baseIdentity(), ready: true, contributionHash: "sha256:abc"})

	result, err := Activate(context.Background(), nil, id, readinessContract())
	if err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	if !result.IdentityVerified || !result.Ready || result.ContributionHash != "sha256:abc" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if next := NextActivationState(result, err); next != StateReady {
		t.Fatalf("next state = %s, want ready", next)
	}
	if !CanTransition(StateActivating, StateReady) {
		t.Fatalf("activating→ready must be a valid transition")
	}
}

func TestActivateIdentityMismatchFails(t *testing.T) {
	id := serveStub(t, &moduleStub{identity: baseIdentity(), ready: true, echoNonce: "wrong-nonce"})

	result, err := Activate(context.Background(), nil, id, readinessContract())
	if err == nil {
		t.Fatalf("expected identity mismatch error")
	}
	if result.IdentityVerified {
		t.Fatalf("identity must not verify on nonce mismatch: %#v", result)
	}
	if next := NextActivationState(result, err); next != StateFailed {
		t.Fatalf("next state = %s, want failed", next)
	}
}

func TestActivateNotReadyFails(t *testing.T) {
	id := serveStub(t, &moduleStub{identity: baseIdentity(), ready: false, detail: "warming up"})

	result, err := Activate(context.Background(), nil, id, readinessContract())
	if err == nil {
		t.Fatalf("expected not-ready error")
	}
	// Identity verified, but the module is not ready → not publishable.
	if !result.IdentityVerified || result.Ready {
		t.Fatalf("unexpected result: %#v", result)
	}
	if next := NextActivationState(result, err); next != StateFailed {
		t.Fatalf("next state = %s, want failed", next)
	}
}

func TestActivateNoReadinessContractReadyAfterHandshake(t *testing.T) {
	id := serveStub(t, &moduleStub{identity: baseIdentity(), ready: false /* readiness endpoint unused */})

	result, err := Activate(context.Background(), nil, id, nil)
	if err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	if !result.IdentityVerified || !result.Ready {
		t.Fatalf("module with no readiness contract must be ready after handshake: %#v", result)
	}
	if next := NextActivationState(result, err); next != StateReady {
		t.Fatalf("next state = %s, want ready", next)
	}
}

func TestActivateWrongCredentialRejected(t *testing.T) {
	id := serveStub(t, &moduleStub{identity: baseIdentity(), ready: true})
	id.Credential = "not-the-credential"

	_, err := Activate(context.Background(), nil, id, readinessContract())
	if err == nil {
		t.Fatalf("expected credential rejection")
	}
}

func TestActivateUnreachableEndpointFails(t *testing.T) {
	id := baseIdentity()
	id.Endpoint = "127.0.0.1:1" // nothing listening

	result, err := Activate(context.Background(), nil, id, readinessContract())
	if err == nil {
		t.Fatalf("expected unreachable-endpoint error")
	}
	if next := NextActivationState(result, err); next != StateFailed {
		t.Fatalf("next state = %s, want failed", next)
	}
}
