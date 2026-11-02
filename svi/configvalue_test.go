package svi

import (
	"encoding/json"
	"strings"
	"testing"
)

const cameraSchema = `{
  "type": "object", "required": ["url"],
  "properties": {
    "url": {"type": "string", "format": "uri", "pattern": "^rtsp://"},
    "password": {"type": "string", "x-terra-secret": true},
    "port": {"type": "integer", "format": "port"},
    "fps": {"type": "number", "minimum": 1, "maximum": 60},
    "mode": {"type": "string", "enum": ["main", "sub"]},
    "tags": {"type": "array", "items": {"type": "string"}},
    "live": {"type": "boolean"},
    "auth": {"type": "object", "required": ["user"], "properties": {
      "user": {"type": "string", "minLength": 1},
      "token": {"type": "string", "x-terra-secret": true}}}
  }
}`

func paths(errs []ConfigError) string {
	var out []string
	for _, err := range errs {
		out = append(out, err.Path)
	}
	return strings.Join(out, ",")
}

func TestValidateConfigAdmitsAGoodConfig(t *testing.T) {
	schema := schemaFrom(t, cameraSchema)
	good := `{"url":"rtsp://10.0.0.5/s1","password":"pw","port":554,"fps":29.97,"mode":"sub","tags":["a"],"live":true,"auth":{"user":"v"}}`
	if errs := ValidateConfig(schema, json.RawMessage(good)); len(errs) != 0 {
		t.Fatalf("errors = %+v", errs)
	}
}

func TestValidateConfigPointsAtEachBadValue(t *testing.T) {
	schema := schemaFrom(t, cameraSchema)
	bad := `{"url":"http://x","port":70000,"fps":0,"mode":"hd","tags":[1],"live":"yes","auth":{},"typo":1}`
	errs := ValidateConfig(schema, json.RawMessage(bad))
	want := "/auth/user,/fps,/live,/mode,/port,/tags/0,/typo,/url"
	if got := paths(errs); got != want {
		t.Fatalf("paths = %s, want %s (%+v)", got, want, errs)
	}
	if errs := ValidateConfig(schema, nil); paths(errs) != "/url" {
		t.Fatalf("empty config = %+v, want only /url required", errs)
	}
	if errs := ValidateConfig(schema, json.RawMessage(`[1]`)); len(errs) != 1 || errs[0].Path != "" {
		t.Fatalf("non-object config = %+v", errs)
	}
	if errs := ValidateConfig(schema, json.RawMessage(`{"url":"rtsp://a","port":5.5}`)); paths(errs) != "/port" {
		t.Fatalf("fractional integer = %+v", errs)
	}
}

func TestSecretsAreRedacted(t *testing.T) {
	schema := schemaFrom(t, cameraSchema)
	if got := strings.Join(SecretPaths(schema), ","); got != "/auth/token,/password" {
		t.Fatalf("secret paths = %s", got)
	}
	redacted := string(RedactConfig(schema, json.RawMessage(`{"url":"rtsp://a","password":"hunter2","auth":{"user":"v","token":"t0k"}}`)))
	if strings.Contains(redacted, "hunter2") || strings.Contains(redacted, "t0k") || !strings.Contains(redacted, `"set":true`) || !strings.Contains(redacted, "rtsp://a") {
		t.Fatalf("redacted = %s", redacted)
	}
	if got := string(RedactConfig(schema, json.RawMessage(`{"url":"rtsp://a"}`))); strings.Contains(got, "set") {
		t.Fatalf("an absent secret must stay absent: %s", got)
	}
}
