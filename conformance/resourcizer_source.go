package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/StellaxiaLab/terra-sdk/svi"
)

// SVISourcePath is the source seam (contract section 6, resourcizer
// framework design §7).
const SVISourcePath = "/terra/svi/source"

type kitSourceResponse struct {
	SourceID  string   `json:"sourceId"`
	SchemaRef string   `json:"schemaRef"`
	Frames    [][]byte `json:"frames"`
	Done      bool     `json:"done"`
	Detail    string   `json:"detail"`
}

func (c checker) source(op string, fields map[string]any) (int, kitSourceResponse, error) {
	fields["op"] = op
	body, _ := json.Marshal(fields)
	status, response, err := c.do(http.MethodPost, SVISourcePath, c.id.Credential, body)
	var decoded kitSourceResponse
	_ = json.Unmarshal(response, &decoded)
	return status, decoded, err
}

func (c checker) sourceSample(plan ResourcizerPlan) (InstanceSample, string, bool) {
	resources, err := c.resources()
	if err != nil {
		return InstanceSample{}, "", false
	}
	for _, sample := range plan.Samples {
		for _, resource := range resources {
			if resource.ResourceID != suffixOf(sample) {
				continue
			}
			for _, endpoint := range resource.Endpoints {
				if endpoint.Direction == svi.DirectionSource {
					return sample, endpoint.EndpointID, true
				}
			}
		}
	}
	return InstanceSample{}, "", false
}

func (c checker) openSource(sample InstanceSample, endpoint string) (string, error) {
	status, response, err := c.source("open", map[string]any{"resourceId": canonical(sample), "endpointId": endpoint})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || response.SourceID == "" {
		return "", fmt.Errorf("source open %s/%s = %d %q, want 200 and a sourceId", canonical(sample), endpoint, status, response.Detail)
	}
	if response.SchemaRef == "" {
		return "", errors.New("source open answered no schemaRef")
	}
	return response.SourceID, nil
}

// sourceLifecycle: not mine is 404; an open source yields frames (or says it
// ended); close takes effect once and succeeds after; a closed id is unknown.
func (c checker) sourceLifecycle(sample InstanceSample, endpoint string) error {
	status, _, err := c.source("open", map[string]any{"resourceId": "svi." + conformanceNode + ".conformance.not-mine", "endpointId": endpoint})
	if err != nil {
		return err
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("source open of a resource the module does not have = %d, want 404", status)
	}
	id, err := c.openSource(sample, endpoint)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	got := false
	for time.Now().Before(deadline) && !got {
		status, response, err := c.source("next", map[string]any{"sourceId": id, "waitMillis": 500})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("next = %d %q, want 200", status, response.Detail)
		}
		got = len(response.Frames) > 0 || response.Done
	}
	if !got {
		return errors.New("an open source produced no frame and did not end within 5s")
	}
	for attempt := 1; attempt <= 2; attempt++ {
		status, response, err := c.source("close", map[string]any{"sourceId": id})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("source close #%d = %d %q, want 200", attempt, status, response.Detail)
		}
	}
	if status, _, err := c.source("next", map[string]any{"sourceId": id, "waitMillis": 10}); err != nil || status != http.StatusNotFound {
		return errors.Join(err, fmt.Errorf("next after close = %d, want 404", status))
	}
	return nil
}

// sourceWithdrawn: withdrawing the instance ends its open source with the
// reason — never as a completed stream, which would read as "done".
func (c checker) sourceWithdrawn(plan ResourcizerPlan, sample InstanceSample, endpoint string) error {
	id, err := c.openSource(sample, endpoint)
	if err != nil {
		return err
	}
	var rest []InstanceSample
	for _, other := range plan.Samples {
		if other.Name != sample.Name {
			rest = append(rest, other)
		}
	}
	if _, err := c.applyInstances(rest); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, response, err := c.source("next", map[string]any{"sourceId": id, "waitMillis": 200})
		if err != nil {
			return err
		}
		if status == http.StatusOK && response.Done {
			return errors.New("a withdrawn instance's source reported a completed stream")
		}
		if status != http.StatusOK {
			if status != http.StatusNotFound && !strings.Contains(response.Detail, "resource_withdrawn") {
				return fmt.Errorf("next after withdrawal = %d %q, want 404 or a resource_withdrawn reason", status, response.Detail)
			}
			_, _, _ = c.source("close", map[string]any{"sourceId": id})
			_, err = c.applyInstances(plan.Samples)
			return err
		}
	}
	return errors.New("a withdrawn instance's source kept producing")
}
