package svi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidResourceFixture(t *testing.T) {
	t.Parallel()

	var resource ResourceDescriptor
	loadFixture(t, "valid", "resource-descriptor.json", &resource)
	if err := ValidateResource(resource); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidEndpointFixture(t *testing.T) {
	t.Parallel()

	var endpoint EndpointDescriptor
	loadFixture(t, "invalid", "endpoint-source-missing-output-schema.json", &endpoint)
	if err := ValidateEndpoint(endpoint); err == nil {
		t.Fatal("expected missing output_schema to fail")
	}
}

func TestResourceRejectsDuplicateEndpoints(t *testing.T) {
	t.Parallel()

	var resource ResourceDescriptor
	loadFixture(t, "valid", "resource-descriptor.json", &resource)
	resource.Endpoints = append(resource.Endpoints, resource.Endpoints[0])
	if err := ValidateResource(resource); err == nil {
		t.Fatal("expected duplicate endpoint to fail")
	}
}

func TestValidateEndpointRejectsNestedMetadata(t *testing.T) {
	t.Parallel()

	var endpoint EndpointDescriptor
	loadFixture(t, "valid", "endpoint-descriptor.json", &endpoint)
	endpoint.Metadata = map[string]any{"nested": map[string]any{"value": true}}
	if err := ValidateEndpoint(endpoint); err == nil {
		t.Fatal("expected nested metadata to fail")
	}
}

func loadFixture(t *testing.T, group, name string, target any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", group, name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
}
