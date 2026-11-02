package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/StellaxiaLab/terra-sdk/svi"
)

// Resourcizer seam paths (contract section 6, resourcizer framework design §4).
const (
	SVIResourcizersPath      = "/terra/svi/resourcizers"
	SVIInstancesPath         = "/terra/svi/instances"
	SVIInstancesValidatePath = "/terra/svi/instances/validate"
)

// conformanceNode is the node id the kit plays when it opens a sink by the
// daemon's canonical id (svi.<node>.<suffix>).
const conformanceNode = "conformance-node"

// InstanceSample is one instance the kit hands the module.
type InstanceSample struct {
	Name   string          `json:"name"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

// ResourcizerPlan turns on the resourcizer rules. The kit cannot invent a
// config a camera accepts, so the module's test names a few.
type ResourcizerPlan struct {
	// Samples must be accepted and reach ready.
	Samples []InstanceSample
	// Invalid must be refused by validate with field errors.
	Invalid []InstanceSample
	// ReadyTimeout bounds the wait for samples to become available. Default 10s.
	ReadyTimeout time.Duration
}

var resourcizerTypeID = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

type kitType struct {
	ID           string           `json:"id"`
	ConfigSchema map[string]any   `json:"configSchema"`
	Endpoints    []map[string]any `json:"endpoints"`
}

type kitTypeList struct {
	ETag  string    `json:"etag"`
	Types []kitType `json:"types"`
}

type kitStatus struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	State     string `json:"state"`
	Reason    string `json:"reason"`
	UpdatedAt string `json:"updatedAt"`
}

type kitSetResult struct {
	Instances []kitStatus `json:"instances"`
}

type kitSinkResponse struct {
	SinkID string `json:"sinkId"`
	Detail string `json:"detail"`
}

// checkResourcizer covers the rules a resourcizer module must keep. Each rule
// names the 9/9 SVI audit shape it guards where it guards one.
func (c checker) checkResourcizer(report *Report, plan ResourcizerPlan) {
	if plan.ReadyTimeout <= 0 {
		plan.ReadyTimeout = 10 * time.Second
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, SVIResourcizersPath},
		{http.MethodPut, SVIInstancesPath},
		{http.MethodPost, SVIInstancesValidatePath},
	} {
		payload := []byte(nil)
		if tc.method != http.MethodGet {
			payload = []byte(`{}`)
		}
		report.add("401 without credential: "+tc.method+" "+tc.path, c.rejectsWith(tc.method, tc.path, "", payload))
	}
	types, err := c.resourcizerTypes()
	report.add("resourcizer types are drawable and stable", err)
	if err != nil {
		return
	}
	schemas := map[string]map[string]any{}
	for _, kind := range types.Types {
		schemas[kind.ID] = kind.ConfigSchema
	}
	secrets := sampleSecrets(plan.Samples, schemas)
	if len(plan.Samples) == 0 {
		report.addSkip("instances apply, settle and publish", "the plan names no sample instance")
	} else {
		first, err := c.applyInstances(plan.Samples)
		report.add("instance set answers one status per instance", err)
		if err == nil {
			second, err := c.applyInstances(plan.Samples)
			report.add("the same set twice changes nothing (status and its time)", sameStatuses(first, second))
			_ = err
		}
		report.add("samples become available resources with valid descriptors", c.waitAvailable(plan))
		report.add("a truncated instance set is refused and changes nothing", c.truncatedSet(plan))
		report.add("a status time moves only with the state", c.statusClock(plan))
	}
	report.add("validate refuses bad configs with field paths", c.validateRules(plan))
	report.add("secret values never leave the module", c.noSecrets(secrets, plan))
	if sample, endpoint, ok := c.sinkSample(plan, types); ok {
		report.add("sink: not mine is 404, open/write/close, close twice succeeds", c.sinkLifecycle(sample, endpoint))
		report.add("sink: withdrawing the instance ends its open sink", c.sinkWithdrawn(plan, sample, endpoint))
	} else {
		report.addSkip("sink rules", "no sample instance has a sink endpoint")
	}
	_, _ = c.applyInstances(nil)
}

func (c checker) resourcizerTypes() (kitTypeList, error) {
	var lists [2]kitTypeList
	for index := range lists {
		status, body, err := c.get(SVIResourcizersPath, c.id.Credential)
		if err != nil {
			return kitTypeList{}, err
		}
		if status != http.StatusOK {
			return kitTypeList{}, fmt.Errorf("status %d, want 200", status)
		}
		if err := json.Unmarshal(body, &lists[index]); err != nil {
			return kitTypeList{}, fmt.Errorf("body is not a type list: %w", err)
		}
	}
	list := lists[0]
	if list.ETag == "" || list.ETag != lists[1].ETag {
		return list, fmt.Errorf("etag %q then %q: it must be present and stable while the types are", list.ETag, lists[1].ETag)
	}
	seen := map[string]bool{}
	for _, kind := range list.Types {
		if !resourcizerTypeID.MatchString(kind.ID) || seen[kind.ID] {
			return list, fmt.Errorf("type id %q must be a distinct lowercase id", kind.ID)
		}
		seen[kind.ID] = true
		if err := svi.CheckConfigSchema(kind.ConfigSchema); err != nil {
			return list, fmt.Errorf("type %s: %w", kind.ID, err)
		}
		if len(kind.Endpoints) == 0 {
			return list, fmt.Errorf("type %s has no endpoints", kind.ID)
		}
	}
	return list, nil
}

func (c checker) applyInstances(samples []InstanceSample) (kitSetResult, error) {
	if samples == nil {
		samples = []InstanceSample{}
	}
	body, _ := json.Marshal(map[string]any{"instances": samples})
	status, response, err := c.do(http.MethodPut, SVIInstancesPath, c.id.Credential, body)
	if err != nil {
		return kitSetResult{}, err
	}
	if status != http.StatusOK {
		return kitSetResult{}, fmt.Errorf("PUT %s = %d, want 200", SVIInstancesPath, status)
	}
	var result kitSetResult
	if err := json.Unmarshal(response, &result); err != nil {
		return result, fmt.Errorf("answer is not an instance set result: %w", err)
	}
	if len(result.Instances) != len(samples) {
		return result, fmt.Errorf("%d statuses for %d instances", len(result.Instances), len(samples))
	}
	for index, got := range result.Instances {
		if got.Name != samples[index].Name {
			return result, fmt.Errorf("status %d names %q, want %q (one per instance, in order)", index, got.Name, samples[index].Name)
		}
		switch got.State {
		case "starting", "ready", "failed":
		default:
			return result, fmt.Errorf("instance %s state %q is not starting, ready or failed", got.Name, got.State)
		}
	}
	return result, nil
}

func sameStatuses(first, second kitSetResult) error {
	for index := range first.Instances {
		a, b := first.Instances[index], second.Instances[index]
		// A Realize may finish between the two calls; then the state moved
		// and the time with it. What must not happen is a fresh time for an
		// unchanged state — that is an instance rebuilt by a no-op set.
		if a.State == b.State && a.UpdatedAt != b.UpdatedAt {
			return fmt.Errorf("instance %s: same state %s but updatedAt %s then %s — the set was re-applied", a.Name, a.State, a.UpdatedAt, b.UpdatedAt)
		}
	}
	return nil
}

func suffixOf(sample InstanceSample) string { return sample.Type + "." + sample.Name }

func (c checker) resources() ([]svi.ResourceDescriptor, error) {
	status, body, err := c.get(SVIResourcesPath, c.id.Credential)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s = %d", SVIResourcesPath, status)
	}
	var resources []svi.ResourceDescriptor
	if err := json.Unmarshal(body, &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func (c checker) waitAvailable(plan ResourcizerPlan) error {
	deadline := time.Now().Add(plan.ReadyTimeout)
	for {
		resources, err := c.resources()
		if err != nil {
			return err
		}
		byID := map[string]svi.ResourceDescriptor{}
		for _, resource := range resources {
			byID[resource.ResourceID] = resource
		}
		missing := ""
		for _, sample := range plan.Samples {
			resource, ok := byID[suffixOf(sample)]
			if !ok || resource.Status != svi.ResourceAvailable {
				missing = suffixOf(sample)
				break
			}
			if err := svi.ValidateResource(resource); err != nil {
				return fmt.Errorf("%s: %w", resource.ResourceID, err)
			}
		}
		if missing == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("resource %s is not available after %s", missing, plan.ReadyTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (c checker) truncatedSet(plan ResourcizerPlan) error {
	status, _, err := c.do(http.MethodPut, SVIInstancesPath, c.id.Credential, []byte(`{"instances":[`))
	if err != nil {
		return err
	}
	if status != http.StatusBadRequest {
		return fmt.Errorf("truncated set = %d, want 400", status)
	}
	return c.waitAvailable(ResourcizerPlan{Samples: plan.Samples, ReadyTimeout: time.Second})
}

func (c checker) statusClock(plan ResourcizerPlan) error {
	first, err := c.applyInstances(plan.Samples)
	if err != nil {
		return err
	}
	second, err := c.applyInstances(plan.Samples)
	if err != nil {
		return err
	}
	for index := range first.Instances {
		if first.Instances[index].UpdatedAt == "" {
			return fmt.Errorf("instance %s has no updatedAt", first.Instances[index].Name)
		}
	}
	return sameStatuses(first, second)
}

func (c checker) validate(sample InstanceSample) (int, map[string]any, error) {
	body, _ := json.Marshal(sample)
	status, response, err := c.do(http.MethodPost, SVIInstancesValidatePath, c.id.Credential, body)
	if err != nil {
		return 0, nil, err
	}
	var decoded map[string]any
	_ = json.Unmarshal(response, &decoded)
	return status, decoded, nil
}

func (c checker) validateRules(plan ResourcizerPlan) error {
	for _, sample := range plan.Samples {
		status, _, err := c.validate(sample)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("validate %s = %d, want 200 for a sample", sample.Name, status)
		}
	}
	for _, sample := range plan.Invalid {
		status, body, err := c.validate(sample)
		if err != nil {
			return err
		}
		if status != http.StatusUnprocessableEntity {
			return fmt.Errorf("validate invalid %s = %d, want 422", sample.Name, status)
		}
		errs, _ := body["errors"].([]any)
		if len(errs) == 0 {
			return fmt.Errorf("validate invalid %s: no errors", sample.Name)
		}
		for _, raw := range errs {
			entry, _ := raw.(map[string]any)
			path, _ := entry["path"].(string)
			if path != "" && !strings.HasPrefix(path, "/") {
				return fmt.Errorf("validate invalid %s: path %q is not a JSON Pointer", sample.Name, path)
			}
		}
	}
	status, _, err := c.validate(InstanceSample{Name: "kit-unknown", Type: "conformance.no-such-type", Config: json.RawMessage(`{}`)})
	if err != nil {
		return err
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("validate of an unknown type = %d, want 404", status)
	}
	return nil
}

// sampleSecrets collects the sample values the schemas mark x-terra-secret.
func sampleSecrets(samples []InstanceSample, schemas map[string]map[string]any) []string {
	var secrets []string
	for _, sample := range samples {
		schema, ok := schemas[sample.Type]
		if !ok {
			continue
		}
		var config map[string]any
		_ = json.Unmarshal(sample.Config, &config)
		for _, path := range svi.SecretPaths(schema) {
			var node any = config
			for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
				object, _ := node.(map[string]any)
				node = object[segment]
			}
			if text, ok := node.(string); ok && len(text) >= 3 {
				secrets = append(secrets, text)
			}
		}
	}
	return secrets
}

func (c checker) noSecrets(secrets []string, plan ResourcizerPlan) error {
	if len(secrets) == 0 {
		return nil
	}
	bodies := map[string][]byte{}
	for _, path := range []string{SVIResourcizersPath, SVIResourcesPath} {
		_, body, err := c.get(path, c.id.Credential)
		if err != nil {
			return err
		}
		bodies["GET "+path] = body
	}
	encoded, _ := json.Marshal(map[string]any{"instances": plan.Samples})
	_, body, err := c.do(http.MethodPut, SVIInstancesPath, c.id.Credential, encoded)
	if err != nil {
		return err
	}
	bodies["PUT "+SVIInstancesPath] = body
	for where, body := range bodies {
		for _, secret := range secrets {
			if bytes.Contains(body, []byte(secret)) {
				return fmt.Errorf("%s answer carries a secret config value", where)
			}
		}
	}
	return nil
}

func (c checker) sinkSample(plan ResourcizerPlan, _ kitTypeList) (InstanceSample, string, bool) {
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
				if endpoint.Direction == svi.DirectionSink {
					return sample, endpoint.EndpointID, true
				}
			}
		}
	}
	return InstanceSample{}, "", false
}

func (c checker) sink(op string, fields map[string]any) (int, kitSinkResponse, error) {
	fields["op"] = op
	body, _ := json.Marshal(fields)
	status, response, err := c.do(http.MethodPost, SVISinkPath, c.id.Credential, body)
	var decoded kitSinkResponse
	_ = json.Unmarshal(response, &decoded)
	return status, decoded, err
}

func canonical(sample InstanceSample) string {
	return "svi." + conformanceNode + "." + suffixOf(sample)
}

func (c checker) openSink(sample InstanceSample, endpoint string) (string, error) {
	status, response, err := c.sink("open", map[string]any{"resourceId": canonical(sample), "endpointId": endpoint})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || response.SinkID == "" {
		return "", fmt.Errorf("open %s/%s = %d %q, want 200 and a sinkId", canonical(sample), endpoint, status, response.Detail)
	}
	return response.SinkID, nil
}

func (c checker) sinkLifecycle(sample InstanceSample, endpoint string) error {
	status, _, err := c.sink("open", map[string]any{"resourceId": "svi." + conformanceNode + ".conformance.not-mine", "endpointId": endpoint})
	if err != nil {
		return err
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("open of a resource the module does not have = %d, want 404", status)
	}
	status, _, err = c.sink("open", map[string]any{"resourceId": canonical(sample), "endpointId": "conformance-no-such-endpoint"})
	if err != nil {
		return err
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("open of an endpoint the instance does not have = %d, want 404", status)
	}
	id, err := c.openSink(sample, endpoint)
	if err != nil {
		return err
	}
	if status, response, err := c.sink("write", map[string]any{"sinkId": id, "sequence": 1, "data": []byte("conformance")}); err != nil || status != http.StatusOK {
		return errors.Join(err, fmt.Errorf("write = %d %q, want 200", status, response.Detail))
	}
	for attempt := 1; attempt <= 2; attempt++ {
		status, response, err := c.sink("close", map[string]any{"sinkId": id})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			// A second close that fails is how a normal end turned into an
			// error at the relay in the 9/9 audit.
			return fmt.Errorf("close #%d = %d %q, want 200", attempt, status, response.Detail)
		}
	}
	if status, _, err := c.sink("write", map[string]any{"sinkId": id, "sequence": 2, "data": []byte("x")}); err != nil || status != http.StatusNotFound {
		return errors.Join(err, fmt.Errorf("write after close = %d, want 404", status))
	}
	return nil
}

func (c checker) sinkWithdrawn(plan ResourcizerPlan, sample InstanceSample, endpoint string) error {
	id, err := c.openSink(sample, endpoint)
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
	status, response, err := c.sink("write", map[string]any{"sinkId": id, "sequence": 1, "data": []byte("x")})
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return errors.New("a sink of a withdrawn instance still accepted a frame")
	}
	if status != http.StatusNotFound && !strings.Contains(response.Detail, "resource_withdrawn") {
		return fmt.Errorf("write after withdrawal = %d %q, want 404 or a resource_withdrawn reason", status, response.Detail)
	}
	if status, _, err := c.sink("close", map[string]any{"sinkId": id}); err != nil || status != http.StatusOK {
		return errors.Join(err, fmt.Errorf("close after withdrawal = %d, want 200", status))
	}
	status, _, err = c.sink("open", map[string]any{"resourceId": canonical(sample), "endpointId": endpoint})
	if err != nil {
		return err
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("open of a withdrawn instance = %d, want 404", status)
	}
	_, err = c.applyInstances(plan.Samples)
	return err
}
