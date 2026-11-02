package svi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// MaxConfigSchemaBytes bounds one type's config schema.
const MaxConfigSchemaBytes = 16 << 10

// The config schema is a subset of JSON Schema a controller can always draw as
// a form (resourcizer framework design §4.2). Anything outside it is refused,
// $ref included: a remote $ref would have the daemon fetch an arbitrary URL.
var (
	schemaRootKeys = keySet("type", "properties", "required", "title", "description", "default", "examples",
		"x-terra-order", "x-terra-group")
	schemaPropertyKeys = keySet("type", "title", "description", "default", "examples", "enum", "const",
		"minimum", "maximum", "minLength", "maxLength", "pattern", "format", "items", "properties", "required",
		"x-terra-secret", "x-terra-ref", "x-terra-order", "x-terra-group", "x-terra-enum-titles")
	schemaScalarTypes = keySet("string", "integer", "number", "boolean")
	schemaItemTypes   = keySet("string", "integer")
	schemaFormats     = keySet("uri", "hostname", "ipv4", "ipv6", "port")
	schemaRefs        = keySet("file", "window", "node", "resource")
)

func keySet(keys ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

// CheckConfigSchema answers whether a type's config schema stays inside the
// drawable subset. A nil error admits it.
func CheckConfigSchema(schema map[string]any) error {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("config_schema is not JSON: %v", err)
	}
	if len(encoded) > MaxConfigSchemaBytes {
		return fmt.Errorf("config_schema is %d bytes; the limit is %d", len(encoded), MaxConfigSchemaBytes)
	}
	if err := unknownKeys("config_schema", schema, schemaRootKeys); err != nil {
		return err
	}
	if schema["type"] != "object" {
		return fmt.Errorf("config_schema.type must be object")
	}
	return checkObject("config_schema", schema, 1)
}

func unknownKeys(at string, node map[string]any, allowed map[string]struct{}) error {
	var unknown []string
	for key := range node {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("%s: %q is outside the drawable schema subset", at, unknown[0])
	}
	return nil
}

// checkObject checks an object's properties. depth 1 is the root; a nested
// object (depth 2) may hold only scalars and arrays.
func checkObject(at string, node map[string]any, depth int) error {
	properties, ok := node["properties"].(map[string]any)
	if !ok || len(properties) == 0 {
		return fmt.Errorf("%s.properties must be a non-empty object", at)
	}
	if required, present := node["required"]; present {
		names, ok := required.([]any)
		if !ok {
			return fmt.Errorf("%s.required must be an array of property names", at)
		}
		for _, name := range names {
			key, ok := name.(string)
			if _, exists := properties[key]; !ok || !exists {
				return fmt.Errorf("%s.required names %v, which is not a property", at, name)
			}
		}
	}
	for name, raw := range properties {
		property, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.properties.%s must be an object", at, name)
		}
		if err := checkProperty(at+".properties."+name, property, depth); err != nil {
			return err
		}
	}
	return nil
}

func checkProperty(at string, property map[string]any, depth int) error {
	if err := unknownKeys(at, property, schemaPropertyKeys); err != nil {
		return err
	}
	kind, _ := property["type"].(string)
	switch {
	case kind == "object":
		if depth > 1 {
			return fmt.Errorf("%s: objects nest one level only", at)
		}
		if err := checkObject(at, property, depth+1); err != nil {
			return err
		}
	case kind == "array":
		items, ok := property["items"].(map[string]any)
		itemType, _ := items["type"].(string)
		if _, allowed := schemaItemTypes[itemType]; !ok || !allowed || len(items) != 1 {
			return fmt.Errorf("%s.items must be {\"type\": \"string\"} or {\"type\": \"integer\"}", at)
		}
	default:
		if _, ok := schemaScalarTypes[kind]; !ok {
			return fmt.Errorf("%s.type %q must be string, integer, number, boolean, object or array", at, kind)
		}
		if _, ok := property["properties"]; ok {
			return fmt.Errorf("%s: properties belong to an object", at)
		}
		if _, ok := property["items"]; ok {
			return fmt.Errorf("%s: items belong to an array", at)
		}
	}
	if format, ok := property["format"]; ok {
		name, _ := format.(string)
		if _, allowed := schemaFormats[name]; !allowed {
			return fmt.Errorf("%s.format %v is not one of uri, hostname, ipv4, ipv6, port", at, format)
		}
	}
	if ref, ok := property["x-terra-ref"]; ok {
		name, _ := ref.(string)
		if _, allowed := schemaRefs[name]; !allowed {
			return fmt.Errorf("%s.x-terra-ref %v is not one of file, window, node, resource", at, ref)
		}
	}
	if pattern, ok := property["pattern"]; ok {
		text, isText := pattern.(string)
		if !isText {
			return fmt.Errorf("%s.pattern must be a string", at)
		}
		if _, err := regexp.Compile(text); err != nil {
			return fmt.Errorf("%s.pattern does not compile: %v", at, err)
		}
	}
	if titles, ok := property["x-terra-enum-titles"]; ok {
		values, _ := property["enum"].([]any)
		labels, isList := titles.([]any)
		if !isList || len(labels) != len(values) {
			return fmt.Errorf("%s.x-terra-enum-titles must match enum one to one", at)
		}
	}
	if secret, ok := property["x-terra-secret"]; ok {
		if flag, isBool := secret.(bool); !isBool || (flag && kind != "string") {
			return fmt.Errorf("%s.x-terra-secret applies to a string and must be a boolean", at)
		}
	}
	return nil
}
