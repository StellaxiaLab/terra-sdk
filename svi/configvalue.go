package svi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// ConfigError points at one config value a schema refuses. Path is a JSON
// Pointer into the config ("/auth/user"); empty means the config as a whole.
type ConfigError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

var hostnamePattern = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// ValidateConfig checks a config against a schema that passed
// CheckConfigSchema and answers every refusal, one per value, in path order.
// A key the schema does not name is refused too: in a form-drawn config it can
// only be a typo or a stale field, and silently keeping it would let it
// surface later as behavior nobody configured.
func ValidateConfig(schema map[string]any, config json.RawMessage) []ConfigError {
	value := map[string]any{}
	if trimmed := bytes.TrimSpace(config); len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		var decoded any
		if err := decoder.Decode(&decoded); err != nil {
			return []ConfigError{{Message: "config is not JSON: " + err.Error()}}
		}
		object, ok := decoded.(map[string]any)
		if !ok {
			return []ConfigError{{Message: "config must be a JSON object"}}
		}
		value = object
	}
	var errs []ConfigError
	validateObject("", schema, value, &errs)
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Path < errs[j].Path })
	return errs
}

func validateObject(at string, schema map[string]any, value map[string]any, errs *[]ConfigError) {
	properties, _ := schema["properties"].(map[string]any)
	if required, ok := schema["required"].([]any); ok {
		for _, name := range required {
			key, _ := name.(string)
			if _, present := value[key]; !present {
				*errs = append(*errs, ConfigError{Path: at + "/" + pointerEscape(key), Message: "required"})
			}
		}
	}
	for key, raw := range value {
		path := at + "/" + pointerEscape(key)
		property, known := properties[key].(map[string]any)
		if !known {
			*errs = append(*errs, ConfigError{Path: path, Message: "not a field of this type"})
			continue
		}
		validateValue(path, property, raw, errs)
	}
}

func validateValue(path string, property map[string]any, raw any, errs *[]ConfigError) {
	fail := func(format string, args ...any) {
		*errs = append(*errs, ConfigError{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	kind, _ := property["type"].(string)
	switch kind {
	case "string":
		text, ok := raw.(string)
		if !ok {
			fail("must be a string")
			return
		}
		if problem := checkString(property, text); problem != "" {
			fail("%s", problem)
		}
	case "integer", "number":
		number, ok := raw.(json.Number)
		if !ok {
			fail("must be a number")
			return
		}
		value, err := number.Float64()
		if err != nil {
			fail("must be a number")
			return
		}
		if kind == "integer" && value != math.Trunc(value) {
			fail("must be a whole number")
			return
		}
		if problem := checkNumber(property, value); problem != "" {
			fail("%s", problem)
		}
	case "boolean":
		if _, ok := raw.(bool); !ok {
			fail("must be true or false")
		}
	case "array":
		items, ok := raw.([]any)
		if !ok {
			fail("must be a list")
			return
		}
		itemType, _ := property["items"].(map[string]any)["type"].(string)
		for index, item := range items {
			validateValue(fmt.Sprintf("%s/%d", path, index), map[string]any{"type": itemType}, item, errs)
		}
	case "object":
		object, ok := raw.(map[string]any)
		if !ok {
			fail("must be an object")
			return
		}
		validateObject(path, property, object, errs)
	}
}

func checkString(property map[string]any, text string) string {
	if problem := checkEnum(property, text); problem != "" {
		return problem
	}
	length := utf8.RuneCountInString(text)
	if limit, ok := asFloat(property["minLength"]); ok && float64(length) < limit {
		return fmt.Sprintf("must be at least %v characters", limit)
	}
	if limit, ok := asFloat(property["maxLength"]); ok && float64(length) > limit {
		return fmt.Sprintf("must be at most %v characters", limit)
	}
	if pattern, ok := property["pattern"].(string); ok {
		if compiled, err := regexp.Compile(pattern); err == nil && !compiled.MatchString(text) {
			return "does not match " + pattern
		}
	}
	switch property["format"] {
	case "uri":
		parsed, err := url.Parse(text)
		if err != nil || parsed.Scheme == "" {
			return "must be a URI with a scheme"
		}
	case "hostname":
		if !hostnamePattern.MatchString(text) {
			return "must be a host name"
		}
	case "ipv4":
		if ip := net.ParseIP(text); ip == nil || ip.To4() == nil {
			return "must be an IPv4 address"
		}
	case "ipv6":
		if ip := net.ParseIP(text); ip == nil || ip.To4() != nil {
			return "must be an IPv6 address"
		}
	}
	return ""
}

func checkNumber(property map[string]any, value float64) string {
	if problem := checkEnum(property, value); problem != "" {
		return problem
	}
	if property["format"] == "port" && (value < 1 || value > 65535) {
		return "must be a port, 1-65535"
	}
	if limit, ok := asFloat(property["minimum"]); ok && value < limit {
		return fmt.Sprintf("must be at least %v", limit)
	}
	if limit, ok := asFloat(property["maximum"]); ok && value > limit {
		return fmt.Sprintf("must be at most %v", limit)
	}
	return ""
}

// checkEnum compares as the value's own kind: strings as text, numbers as
// float64 whatever Go type the schema literal had.
func checkEnum(property map[string]any, value any) string {
	allowed, hasEnum := property["enum"].([]any)
	if constant, ok := property["const"]; ok {
		allowed, hasEnum = []any{constant}, true
	}
	if !hasEnum {
		return ""
	}
	for _, candidate := range allowed {
		if sameValue(candidate, value) {
			return ""
		}
	}
	names := make([]string, 0, len(allowed))
	for _, candidate := range allowed {
		names = append(names, fmt.Sprint(candidate))
	}
	return "must be one of " + strings.Join(names, ", ")
}

func sameValue(candidate, value any) bool {
	if text, ok := value.(string); ok {
		other, isText := candidate.(string)
		return isText && other == text
	}
	number, ok := value.(float64)
	other, isNumber := asFloat(candidate)
	return ok && isNumber && number == other
}

func asFloat(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	}
	return 0, false
}

func pointerEscape(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// SecretPaths lists the JSON Pointers of the schema's x-terra-secret fields.
func SecretPaths(schema map[string]any) []string {
	var paths []string
	var walk func(at string, node map[string]any)
	walk = func(at string, node map[string]any) {
		properties, _ := node["properties"].(map[string]any)
		for key, raw := range properties {
			property, _ := raw.(map[string]any)
			path := at + "/" + pointerEscape(key)
			if secret, _ := property["x-terra-secret"].(bool); secret {
				paths = append(paths, path)
			}
			if property["type"] == "object" {
				walk(path, property)
			}
		}
	}
	walk("", schema)
	sort.Strings(paths)
	return paths
}

// RedactConfig replaces every secret value that is present with
// {"set": true}, so a listing can say a password is configured without saying
// what it is. A config that does not decode is answered as null.
func RedactConfig(schema map[string]any, config json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(config, &value); err != nil {
		return json.RawMessage("null")
	}
	for _, path := range SecretPaths(schema) {
		redactAt(value, strings.Split(strings.TrimPrefix(path, "/"), "/"))
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return encoded
}

func redactAt(node any, segments []string) {
	object, ok := node.(map[string]any)
	if !ok || len(segments) == 0 {
		return
	}
	key := strings.ReplaceAll(strings.ReplaceAll(segments[0], "~1", "/"), "~0", "~")
	if len(segments) == 1 {
		if _, present := object[key]; present {
			object[key] = map[string]any{"set": true}
		}
		return
	}
	redactAt(object[key], segments[1:])
}
