package modulesdk_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
	modulesdk "github.com/StellaxiaLab/terra-sdk/modulesdk"
	coresvi "github.com/StellaxiaLab/terra-sdk/svi"
)

func sviGet(t *testing.T, identity modulert.WorkloadIdentity, credential string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "http://"+identity.Endpoint+modulert.SVIResourcesPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if credential != "" {
		request.Header.Set(modulert.CredentialHeader, credential)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

// The SVI resources control path serves the module's descriptors to the daemon
// and to nobody else: the workload credential is the whole boundary, exactly as
// for the handshake.
func TestSVIResourcesControlPath(t *testing.T) {
	identity := hostIdentity(t, "io.terra.sample.svi")
	serve(t, modulesdk.Config{
		Identity: identity,
		SVIResources: func(context.Context) ([]coresvi.ResourceDescriptor, error) {
			return []coresvi.ResourceDescriptor{{
				Kind:          "io.camera",
				CanonicalName: "io/camera/camera-default",
				DisplayName:   "Default camera",
			}}, nil
		},
	})

	if response := sviGet(t, identity, ""); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("uncredentialed poll = %d, want 401", response.StatusCode)
	}

	response := sviGet(t, identity, identity.Credential)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("poll = %d", response.StatusCode)
	}
	var resources []coresvi.ResourceDescriptor
	if err := json.NewDecoder(response.Body).Decode(&resources); err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].CanonicalName != "io/camera/camera-default" {
		t.Fatalf("resources = %+v", resources)
	}
}

// A module that cannot enumerate answers with an error, never a fabricated
// empty set: the daemon must be able to tell "no resources" from "broken".
func TestSVIResourcesErrorIsAnError(t *testing.T) {
	identity := hostIdentity(t, "io.terra.sample.svi-err")
	serve(t, modulesdk.Config{
		Identity: identity,
		SVIResources: func(context.Context) ([]coresvi.ResourceDescriptor, error) {
			return nil, errors.New("enumeration backend is down")
		},
	})

	if response := sviGet(t, identity, identity.Credential); response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("failed poll = %d, want 500", response.StatusCode)
	}
}

// A module without the callback does not serve the path at all — the daemon
// treats 404 as "not a resource provider", not as an error.
func TestSVIResourcesAbsentWithoutCallback(t *testing.T) {
	identity := hostIdentity(t, "io.terra.sample.no-svi")
	serve(t, modulesdk.Config{Identity: identity})

	if response := sviGet(t, identity, identity.Credential); response.StatusCode != http.StatusNotFound {
		t.Fatalf("pathless poll = %d, want 404", response.StatusCode)
	}
}
