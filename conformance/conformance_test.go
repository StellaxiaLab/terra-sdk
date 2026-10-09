package conformance_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/StellaxiaLab/terra-sdk/conformance"
	"github.com/StellaxiaLab/terra-sdk/conformance/conformancetest"
	"github.com/StellaxiaLab/terra-sdk/modulert"
	"github.com/StellaxiaLab/terra-sdk/modulesdk"
)

const sampleID = "io.terra.conformance.sample"

func python(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("python이 없다 — 문서의 예시를 돌릴 수 없다")
	return ""
}

// The Python example the contract document carries is the reference for "any
// language is enough": it must pass the kit unchanged.
func TestDocumentedPythonExamplePasses(t *testing.T) {
	conformancetest.Run(t, conformance.Spec{
		Command:       python(t),
		Args:          []string{"../docs/contracts/examples/module-host-minimal.py"},
		ModuleID:      sampleID,
		OperationPath: "/api/modules/" + sampleID + "/v1/status",
	})
}

// A Go module built on this SDK is checked the same way as any other process:
// the test binary re-executes itself as the module.
func TestGoModuleOnTheSDKPasses(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERRA_CONFORMANCE_HELPER", "1")
	conformancetest.Run(t, conformance.Spec{
		Command:  exe,
		Args:     []string{"-test.run=^TestHelperModuleProcess$"},
		ModuleID: sampleID,
	})
}

func TestHelperModuleProcess(t *testing.T) {
	if os.Getenv("TERRA_CONFORMANCE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	identity, err := modulesdk.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	server, err := modulesdk.Listen(modulesdk.Config{
		Identity:  identity,
		Readiness: func(context.Context) (bool, string) { return true, "" },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = server.Serve(context.Background())
}

// fakeModule is a deliberately flawed module: each field switches one contract
// violation on.
type fakeModule struct {
	id          conformance.Identity
	wrongNonce  bool
	notReady    bool
	skipAuth    bool
	leakOn401   bool
	wrongStatus int
}

func (m fakeModule) handler() http.Handler {
	mux := http.NewServeMux()
	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !m.skipAuth && r.Header.Get(conformance.CredentialHeader) != m.id.Credential {
				status := http.StatusUnauthorized
				if m.wrongStatus != 0 {
					status = m.wrongStatus
				}
				w.WriteHeader(status)
				if m.leakOn401 {
					_, _ = w.Write([]byte(m.id.Nonce))
				}
				return
			}
			next(w, r)
		}
	}
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc(conformance.HandshakePath, guard(func(w http.ResponseWriter, _ *http.Request) {
		nonce := m.id.Nonce
		if m.wrongNonce {
			nonce = "regenerated"
		}
		write(w, map[string]string{"instanceId": m.id.InstanceID, "nonce": nonce})
	}))
	mux.HandleFunc(conformance.ReadinessPath, guard(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"ready": !m.notReady, "detail": "warming up"})
	}))
	return mux
}

func checkFake(t *testing.T, mutate func(*fakeModule)) conformance.Report {
	t.Helper()
	id, err := conformance.NewIdentity(sampleID, 1)
	if err != nil {
		t.Fatal(err)
	}
	module := fakeModule{id: id}
	if mutate != nil {
		mutate(&module)
	}
	server := httptest.NewUnstartedServer(module.handler())
	listener, err := newListener(id.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return conformance.Check(context.Background(), id, conformance.CheckOptions{})
}

func TestCheckAcceptsAConformingModule(t *testing.T) {
	if report := checkFake(t, nil); !report.OK() {
		t.Fatalf("conforming module rejected:\n%s", report)
	}
}

func TestCheckCatchesEachViolation(t *testing.T) {
	cases := map[string]struct {
		mutate func(*fakeModule)
		want   string
	}{
		"handshake regenerates the nonce": {func(m *fakeModule) { m.wrongNonce = true }, "handshake echoes"},
		"readiness reports not ready":     {func(m *fakeModule) { m.notReady = true }, "readiness answers ready"},
		"no credential check":             {func(m *fakeModule) { m.skipAuth = true }, "401 without credential"},
		"401 body leaks the nonce":        {func(m *fakeModule) { m.leakOn401 = true }, "401 without credential"},
		"403 instead of 401":              {func(m *fakeModule) { m.wrongStatus = http.StatusForbidden }, "401 without credential"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			report := checkFake(t, tc.mutate)
			if report.OK() {
				t.Fatalf("violation not detected:\n%s", report)
			}
			if !strings.Contains(report.String(), "FAIL "+tc.want) {
				t.Fatalf("expected a FAIL on %q:\n%s", tc.want, report)
			}
		})
	}
}

// The kit spells the contract out on its own so it works without the SDK. This
// pins the two copies together: if the SDK's wire constants move, the kit turns
// red instead of silently certifying a different contract.
func TestKitLiteralsMatchTheSDKContract(t *testing.T) {
	pairs := [][2]string{
		{conformance.EnvModuleID, modulert.EnvModuleID},
		{conformance.EnvInstanceID, modulert.EnvInstanceID},
		{conformance.EnvEpoch, modulert.EnvEpoch},
		{conformance.EnvNonce, modulert.EnvNonce},
		{conformance.EnvCredential, modulert.EnvCredential},
		{conformance.EnvEndpoint, modulert.EnvEndpoint},
		{conformance.CredentialHeader, modulert.CredentialHeader},
		{conformance.HandshakePath, modulert.HandshakePath},
		{conformance.ReadinessPath, modulert.ReadinessPath},
	}
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			t.Errorf("kit %q != sdk %q", pair[0], pair[1])
		}
	}
}

func TestIdentityEnvIsTheSixVariables(t *testing.T) {
	id, err := conformance.NewIdentity(sampleID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(id.Env()); got != 6 {
		t.Fatalf("Env has %d entries, want 6", got)
	}
	again, _ := conformance.NewIdentity(sampleID, 8)
	if id.Nonce == again.Nonce || id.Credential == again.Credential || id.InstanceID == again.InstanceID {
		t.Fatal("identities must be fresh per issue")
	}
}
