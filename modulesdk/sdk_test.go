package modulesdk_test

import (
	"context"
	"testing"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
	modulesdk "github.com/StellaxiaLab/terra-sdk/modulesdk"
)

// hostIdentity mirrors what the Runtime does before launching a workload: reserve
// a loopback endpoint and issue an identity for it.
func hostIdentity(t *testing.T, moduleID string) modulert.WorkloadIdentity {
	t.Helper()
	endpoint, err := modulert.AllocateLoopbackEndpoint()
	if err != nil {
		t.Fatalf("allocate endpoint: %v", err)
	}
	identity, err := modulert.NewIdentityIssuer().Issue(moduleID, 1, endpoint)
	if err != nil {
		t.Fatalf("issue identity: %v", err)
	}
	return identity
}

func serve(t *testing.T, cfg modulesdk.Config) {
	t.Helper()
	server, err := modulesdk.Listen(cfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Serve(ctx) }()
	t.Cleanup(func() { _ = server.Close() })
}

// TestSDKAndRuntimeActivateEndToEnd wires B1+B2+B3 together: the SDK hosts a
// module at its issued identity, and the Runtime activates it to ready.
func TestSDKAndRuntimeActivateEndToEnd(t *testing.T) {
	identity := hostIdentity(t, "io.terra.sample")
	serve(t, modulesdk.Config{
		Identity:         identity,
		ContributionHash: "sha256:test",
		Readiness:        func(context.Context) (bool, string) { return true, "" },
	})

	readiness := &modulert.Readiness{OperationID: "io.terra.sample.health.get", TimeoutMS: 2000}
	result, err := modulert.Activate(context.Background(), nil, identity, readiness)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if !result.IdentityVerified || !result.Ready || result.ContributionHash != "sha256:test" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if next := modulert.NextActivationState(result, err); next != modulert.StateReady {
		t.Fatalf("next state = %s, want ready", next)
	}
}

// TestSDKNotReadyBlocksActivation: a module that reports not-ready must not pass
// the readiness probe.
func TestSDKNotReadyBlocksActivation(t *testing.T) {
	identity := hostIdentity(t, "io.terra.sample")
	serve(t, modulesdk.Config{
		Identity:  identity,
		Readiness: func(context.Context) (bool, string) { return false, "warming up" },
	})

	result, err := modulert.Activate(context.Background(), nil, identity, &modulert.Readiness{OperationID: "io.terra.sample.health.get", TimeoutMS: 2000})
	if err == nil {
		t.Fatalf("expected not-ready error")
	}
	if !result.IdentityVerified || result.Ready {
		t.Fatalf("identity should verify but module not ready: %#v", result)
	}
	if next := modulert.NextActivationState(result, err); next != modulert.StateFailed {
		t.Fatalf("next state = %s, want failed", next)
	}
}

// TestSDKGuardsCredential: the SDK rejects a caller without the workload
// credential, so a wrong credential fails the handshake.
func TestSDKGuardsCredential(t *testing.T) {
	identity := hostIdentity(t, "io.terra.sample")
	serve(t, modulesdk.Config{Identity: identity, Readiness: func(context.Context) (bool, string) { return true, "" }})

	tampered := identity
	tampered.Credential = "wrong-credential"
	if _, err := modulert.Activate(context.Background(), nil, tampered, nil); err == nil {
		t.Fatalf("expected credential rejection")
	}
}

func TestListenRejectsIncompleteIdentity(t *testing.T) {
	if _, err := modulesdk.Listen(modulesdk.Config{Identity: modulert.WorkloadIdentity{Credential: "c"}}); err == nil {
		t.Fatalf("missing endpoint must be rejected")
	}
	if _, err := modulesdk.Listen(modulesdk.Config{Identity: modulert.WorkloadIdentity{Endpoint: "127.0.0.1:0"}}); err == nil {
		t.Fatalf("missing credential must be rejected")
	}
}
