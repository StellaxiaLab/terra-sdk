package modulesdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
)

func coreHost(t *testing.T) (*CoreClient, *modulert.CoreInvocation) {
	t.Helper()
	var seen modulert.CoreInvocation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != modulert.CoreInvokePath {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get(modulert.CredentialHeader) != "wcred" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"CORE_CALLER_UNKNOWN","message":"unknown"}}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&seen)
		switch seen.OperationID {
		case "terra.node.identity.get":
			_, _ = w.Write([]byte(`{"output":{"node_id":"node-sdk"}}`))
		case "terra.storage.wipe":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"CORE_OPERATION_DENIED","message":"not granted"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"CORE_OPERATION_UNKNOWN","message":"no such operation"}}`))
		}
	}))
	t.Cleanup(server.Close)
	return NewCoreClient(strings.TrimPrefix(server.URL, "http://"), "wcred", nil), &seen
}

func TestCoreClientInvokes(t *testing.T) {
	client, seen := coreHost(t)
	result, err := client.Invoke(context.Background(), modulert.CoreInvocation{
		OperationID: "terra.node.identity.get", VersionRange: ">=1.0.0 <2.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Output), "node-sdk") {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if seen.VersionRange != ">=1.0.0 <2.0.0" {
		t.Fatalf("version range not forwarded: %+v", seen)
	}
}

func TestCoreClientMapsRefusals(t *testing.T) {
	client, _ := coreHost(t)
	if _, err := client.Invoke(context.Background(), modulert.CoreInvocation{OperationID: "terra.storage.wipe"}); !errors.Is(err, modulert.ErrCoreOperationDenied) {
		t.Fatalf("denied = %v", err)
	}
	if _, err := client.Invoke(context.Background(), modulert.CoreInvocation{OperationID: "terra.unknown"}); !errors.Is(err, ErrCoreUnavailable) {
		t.Fatalf("unknown = %v", err)
	}
	wrong := NewCoreClient(client.endpoint, "stolen", nil)
	if _, err := wrong.Invoke(context.Background(), modulert.CoreInvocation{OperationID: "terra.node.identity.get"}); !errors.Is(err, modulert.ErrCoreOperationDenied) {
		t.Fatalf("unverified caller = %v", err)
	}
}

func TestCoreFromEnvRequiresTheHostPlane(t *testing.T) {
	t.Setenv(modulert.EnvHostEndpoint, "")
	t.Setenv(modulert.EnvCredential, "wcred")
	if _, err := CoreFromEnv(); !errors.Is(err, ErrCoreUnavailable) {
		t.Fatalf("missing endpoint = %v", err)
	}

	t.Setenv(modulert.EnvHostEndpoint, "127.0.0.1:1")
	client, err := CoreFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if client.endpoint != "127.0.0.1:1" || client.credential != "wcred" {
		t.Fatalf("unexpected client: %+v", client)
	}
}
