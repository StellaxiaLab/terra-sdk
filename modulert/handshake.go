package modulert

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Handshake wire contract shared with terra-module-sdk (design §12.1). The
// Runtime probes the launched module's loopback endpoint at these fixed paths,
// presenting the workload credential in CredentialHeader; the module echoes the
// issued nonce to prove identity and reports its readiness and contribution
// hash. These are internal control-plane paths, distinct from the module's own
// operation routes.
const (
	HandshakePath    = "/terra/handshake"
	ReadinessPath    = "/terra/readiness"
	CredentialHeader = "X-Terra-Workload-Credential"
	// SVIResourcesPath is where a module that provides SVI resources serves its
	// current resource descriptors (JSON array per the terra-svi
	// resource-descriptor schema). Like the paths above it is host↔module
	// control plane: the daemon's remote provider adapter polls it with the
	// workload credential, and it is never published as a Gateway route. The
	// payload contract is defined by terra-svi so this package stays
	// dependency-free.
	SVIResourcesPath = "/terra/svi/resources"
	// CoreInvokePath is the reverse direction: the path on the HOST's capability
	// endpoint (EnvHostEndpoint) where a module POSTs a CoreInvocation to invoke
	// a Core capability (design §14.3 Module SDK → Host Capability Broker →
	// Host Adapter). The module authenticates with its workload credential; the
	// host resolves the caller by that credential and applies the broker's
	// permission gate before anything executes.
	CoreInvokePath = "/terra/core/invoke"
)

// defaultReadinessTimeout bounds a readiness probe when the manifest declares no
// timeout (design §10.2 readiness.timeoutMs example is 15000).
const defaultReadinessTimeout = 15 * time.Second

// HandshakeResponse is the module's reply to the identity handshake. InstanceID
// and Nonce must match the issued identity to prove the module is the launched
// instance; ContributionHash is the module's self-reported contribution snapshot
// digest, recorded by the Runtime for Phase C (Contribution Registry).
//
// WindowOrigin is the optional UI origin of a window-mode GUI app (창 모드 앱
// 런처 설계 §5.1): a module that opened its own top-level UI server reports
// where, and the host forwards it inside the provider publication. The module
// is the only party that knows this address — it bound the listener — which is
// why it travels the handshake rather than the manifest. The Gateway validates
// it (loopback only) before anything trusts it.
type HandshakeResponse struct {
	InstanceID       string `json:"instanceId"`
	Nonce            string `json:"nonce"`
	ContributionHash string `json:"contributionHash,omitempty"`
	WindowOrigin     string `json:"windowOrigin,omitempty"`
}

// ReadinessResponse is the module's reply to the readiness probe.
type ReadinessResponse struct {
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

// HandshakeResult is the outcome of activating a launched workload. It gates the
// module's activating→ready transition (see NextActivationState).
type HandshakeResult struct {
	IdentityVerified bool
	Ready            bool
	ContributionHash string
	WindowOrigin     string
}

// Activate runs the identity handshake then the readiness probe against a
// launched workload over its loopback endpoint (design §12.1: R→M identity
// handshake, R→M readiness probe). It presents the workload credential, verifies
// the module echoes the issued instance id and nonce, then probes readiness
// within the manifest timeout. A nil client uses http.DefaultClient. The
// returned result gates the activating→ready transition; the caller applies the
// State change via NextActivationState.
//
// A module with no readiness contract (readiness nil or without an operationId)
// is considered ready once its identity handshake succeeds.
func Activate(ctx context.Context, client *http.Client, identity WorkloadIdentity, readiness *Readiness) (HandshakeResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout(readiness))
	defer cancel()

	var handshake HandshakeResponse
	if err := getJSON(ctx, client, identity, HandshakePath, &handshake); err != nil {
		return HandshakeResult{}, fmt.Errorf("identity handshake: %w", err)
	}
	if handshake.InstanceID != identity.InstanceID || handshake.Nonce != identity.Nonce {
		return HandshakeResult{ContributionHash: handshake.ContributionHash},
			fmt.Errorf("identity handshake mismatch for %s", identity.ModuleID)
	}
	result := HandshakeResult{
		IdentityVerified: true,
		ContributionHash: handshake.ContributionHash,
		WindowOrigin:     strings.TrimSpace(handshake.WindowOrigin),
	}

	if readiness == nil || readiness.OperationID == "" {
		result.Ready = true
		return result, nil
	}

	var probe ReadinessResponse
	if err := getJSON(ctx, client, identity, ReadinessPath, &probe); err != nil {
		return result, fmt.Errorf("readiness probe: %w", err)
	}
	result.Ready = probe.Ready
	if !probe.Ready {
		return result, fmt.Errorf("module %s not ready: %s", identity.ModuleID, probe.Detail)
	}
	return result, nil
}

// NextActivationState maps a handshake result to the lifecycle State a module in
// StateActivating moves to: StateReady on a verified, ready module; StateFailed
// otherwise (identity mismatch, probe error or not-ready). The caller must be in
// StateActivating — CanTransition(StateActivating, next) holds for both outcomes.
func NextActivationState(result HandshakeResult, err error) State {
	if err == nil && result.IdentityVerified && result.Ready {
		return StateReady
	}
	return StateFailed
}

func readinessTimeout(readiness *Readiness) time.Duration {
	if readiness != nil && readiness.TimeoutMS > 0 {
		return time.Duration(readiness.TimeoutMS) * time.Millisecond
	}
	return defaultReadinessTimeout
}

func getJSON(ctx context.Context, client *http.Client, identity WorkloadIdentity, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+identity.Endpoint+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set(CredentialHeader, identity.Credential)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
