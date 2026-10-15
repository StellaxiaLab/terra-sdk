package conformance

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/StellaxiaLab/terra-sdk/svi"
)

// checkSVI covers the rules the contract (section 6) actually states for the
// SVI control paths:
//
//   - both paths demand the workload credential like every other path
//   - /terra/svi/resources answers 200 with a JSON array of valid resource
//     descriptors
//
// What the contract does not pin down for /terra/svi/sink (the body shapes of
// open/write/close, the sinkId) is not exercised; it is reported as skipped
// with the reason instead of being guessed.
func (c checker) checkSVI(report *Report) {
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, SVIResourcesPath},
		{http.MethodPost, SVISinkPath},
	} {
		payload := []byte(nil)
		if tc.method == http.MethodPost {
			payload = []byte(`{}`)
		}
		report.add("401 without credential: "+tc.path, c.rejectsWith(tc.method, tc.path, "", payload))
		report.add("401 with wrong credential: "+tc.path, c.rejectsWith(tc.method, tc.path, c.id.Credential+"x", payload))
	}
	report.add("svi resources are valid descriptors: "+SVIResourcesPath, c.sviResources())
	report.addSkip("svi sink open/write/close: "+SVISinkPath, "the contract does not specify the request/response bodies tightly enough to check")
}

func (c checker) sviResources() error {
	status, body, err := c.get(SVIResourcesPath, c.id.Credential)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("status %d, want 200", status)
	}
	var resources []svi.ResourceDescriptor
	if err := json.Unmarshal(body, &resources); err != nil {
		return fmt.Errorf("body is not a JSON array of resource descriptors: %w", err)
	}
	for i, resource := range resources {
		if err := svi.ValidateResource(resource); err != nil {
			return fmt.Errorf("resource %d (%q): %w", i, resource.ResourceID, err)
		}
	}
	return nil
}
