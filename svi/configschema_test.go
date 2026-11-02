package svi

import (
	"encoding/json"
	"strings"
	"testing"
)

func schemaFrom(t *testing.T, text string) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal([]byte(text), &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestConfigSchemaSubsetAdmitsTheDrawableShapes(t *testing.T) {
	schema := schemaFrom(t, `{
	  "type": "object", "required": ["url"], "title": "camera",
	  "properties": {
	    "url": {"type": "string", "format": "uri", "pattern": "^rtsp://"},
	    "password": {"type": "string", "x-terra-secret": true},
	    "port": {"type": "integer", "format": "port", "minimum": 1, "maximum": 65535},
	    "mode": {"type": "string", "enum": ["a", "b"], "x-terra-enum-titles": ["A", "B"]},
	    "tags": {"type": "array", "items": {"type": "string"}},
	    "source": {"type": "string", "x-terra-ref": "file"},
	    "auth": {"type": "object", "properties": {"user": {"type": "string"}}, "required": ["user"]}
	  }
	}`)
	if err := CheckConfigSchema(schema); err != nil {
		t.Fatal(err)
	}
}

func TestConfigSchemaSubsetRefusesWhatAFormCannotDraw(t *testing.T) {
	cases := map[string]string{
		"remote ref":          `{"type":"object","properties":{"a":{"$ref":"https://example.com/s.json"}}}`,
		"root ref":            `{"type":"object","$ref":"#/x","properties":{"a":{"type":"string"}}}`,
		"oneOf":               `{"type":"object","properties":{"a":{"oneOf":[{"type":"string"}]}}}`,
		"root not object":     `{"type":"string","properties":{"a":{"type":"string"}}}`,
		"no properties":       `{"type":"object"}`,
		"deep object":         `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"object","properties":{"c":{"type":"string"}}}}}}}`,
		"array of objects":    `{"type":"object","properties":{"a":{"type":"array","items":{"type":"object"}}}}`,
		"unknown format":      `{"type":"object","properties":{"a":{"type":"string","format":"email"}}}`,
		"unknown ref widget":  `{"type":"object","properties":{"a":{"type":"string","x-terra-ref":"camera"}}}`,
		"bad pattern":         `{"type":"object","properties":{"a":{"type":"string","pattern":"(?<=x)"}}}`,
		"required not a prop": `{"type":"object","required":["b"],"properties":{"a":{"type":"string"}}}`,
		"titles mismatch":     `{"type":"object","properties":{"a":{"type":"string","enum":["x","y"],"x-terra-enum-titles":["X"]}}}`,
		"secret on integer":   `{"type":"object","properties":{"a":{"type":"integer","x-terra-secret":true}}}`,
		"no type":             `{"type":"object","properties":{"a":{"title":"untyped"}}}`,
	}
	for name, text := range cases {
		if err := CheckConfigSchema(schemaFrom(t, text)); err == nil {
			t.Errorf("%s: admitted", name)
		}
	}
}

func TestConfigSchemaSubsetBoundsSize(t *testing.T) {
	big := `{"type":"object","properties":{"a":{"type":"string","description":"` + strings.Repeat("x", MaxConfigSchemaBytes) + `"}}}`
	if err := CheckConfigSchema(schemaFrom(t, big)); err == nil {
		t.Fatal("an oversized schema was admitted")
	}
}
