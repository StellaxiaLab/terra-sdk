package modulert

import (
	"net"
	"testing"
)

func TestIdentityIssuerMintsUniqueIdentities(t *testing.T) {
	issuer := NewIdentityIssuer()
	a, err := issuer.Issue("io.terra.backup", 7, "127.0.0.1:54000")
	if err != nil {
		t.Fatalf("issue a: %v", err)
	}
	b, err := issuer.Issue("io.terra.backup", 7, "127.0.0.1:54000")
	if err != nil {
		t.Fatalf("issue b: %v", err)
	}
	if a.ModuleID != "io.terra.backup" || a.Epoch != 7 || a.Endpoint != "127.0.0.1:54000" {
		t.Fatalf("issued identity fields wrong: %#v", a)
	}
	// Every launch gets a fresh, non-reused identity.
	if a.InstanceID == b.InstanceID || a.Nonce == b.Nonce || a.Credential == b.Credential {
		t.Fatalf("identities must not be reused across launches: %#v vs %#v", a, b)
	}
	if a.Nonce == "" || a.Credential == "" || a.InstanceID == "" {
		t.Fatalf("identity has empty secret fields: %#v", a)
	}
}

func TestIdentityIssuerRejectsEmptyInputs(t *testing.T) {
	issuer := NewIdentityIssuer()
	if _, err := issuer.Issue("", 1, "127.0.0.1:1"); err == nil {
		t.Fatalf("empty module id must be rejected")
	}
	if _, err := issuer.Issue("io.terra.backup", 1, ""); err == nil {
		t.Fatalf("empty endpoint must be rejected")
	}
}

func TestWorkloadIdentityEnvRoundTrip(t *testing.T) {
	original := WorkloadIdentity{
		ModuleID:   "io.terra.backup",
		InstanceID: "inst-abc",
		Epoch:      42,
		Nonce:      "nonce-xyz",
		Credential: "cred-123",
		Endpoint:   "127.0.0.1:54001",
	}
	env := original.Env()
	restored, err := IdentityFromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	if restored != original {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", restored, original)
	}
}

func TestIdentityFromEnvRejectsIncomplete(t *testing.T) {
	full := WorkloadIdentity{ModuleID: "m", InstanceID: "i", Epoch: 1, Nonce: "n", Credential: "c", Endpoint: "127.0.0.1:1"}.Env()

	// Drop each required field in turn; all must be rejected.
	for _, missing := range []string{EnvModuleID, EnvInstanceID, EnvNonce, EnvCredential, EnvEndpoint} {
		lookup := func(key string) string {
			if key == missing {
				return ""
			}
			return full[key]
		}
		if _, err := IdentityFromEnv(lookup); err == nil {
			t.Fatalf("missing %s must be rejected", missing)
		}
	}
	// A non-numeric epoch is rejected.
	if _, err := IdentityFromEnv(func(key string) string {
		if key == EnvEpoch {
			return "notnum"
		}
		return full[key]
	}); err == nil {
		t.Fatalf("invalid epoch must be rejected")
	}
}

func TestAllocateLoopbackEndpointIsLoopback(t *testing.T) {
	endpoint, err := AllocateLoopbackEndpoint()
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		t.Fatalf("endpoint %q not host:port: %v", endpoint, err)
	}
	if host != LoopbackHost {
		t.Fatalf("endpoint host = %q, want %q", host, LoopbackHost)
	}
	if port == "0" || port == "" {
		t.Fatalf("endpoint port not assigned: %q", endpoint)
	}
	// The released port must be bindable by the workload.
	listener, err := net.Listen("tcp", endpoint)
	if err != nil {
		t.Fatalf("released endpoint not bindable: %v", err)
	}
	_ = listener.Close()
}
