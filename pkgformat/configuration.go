package pkgformat

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Configuration is the manifest's configuration block.
type Configuration struct {
	// Schema is the module's configuration schema, held as decoded JSON so the
	// manifest round-trips it untouched. ParseConfigSchema reads it.
	Schema map[string]any `json:"schema"`
}

// ConfigSchemaMaxBytes bounds the encoded schema .
const ConfigSchemaMaxBytes = 64 << 10

// ConfigSecretKeyword marks a property whose value is written but never
// read back: read operations answer {"set": true} instead of the value.
const ConfigSecretKeyword = "x-terra-secret"

// ConfigRestartKeyword is the module's promise that a property can be
// applied without a restart. It is recorded; a host may restart anyway.
const ConfigRestartKeyword = "x-terra-restart"

const ConfigTypeObject = "object"

const ConfigTypeString = "string"

const ConfigTypeInteger = "integer"

const ConfigTypeNumber = "number"

const ConfigTypeBoolean = "boolean"

const ConfigTypeArray = "array"

// configSchemaKeywords is the subset. Annotation keywords (title, description,
// $comment, examples) carry no constraint and are accepted so a GUI can label
// its fields.
var configSchemaKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "enum": true,
	"minimum": true, "maximum": true, "minLength": true, "maxLength": true,
	"pattern": true, "format": true, "default": true, "items": true, "additionalProperties": true,
	"title": true, "description": true, "$comment": true, "examples": true,
	ConfigSecretKeyword: true, ConfigRestartKeyword: true,
}

var configFormats = map[string]bool{"uri": true, "hostname": true, "ipv4": true}

var hostnamePattern = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// ConfigSchema is a parsed, checked configuration schema node.
type ConfigSchema struct {
	Type                 string
	Title                string
	Description          string
	Properties           map[string]*ConfigSchema
	Required             []string
	Enum                 []any
	Minimum              *float64
	Maximum              *float64
	MinLength            *int
	MaxLength            *int
	Pattern              *regexp.Regexp
	Format               string
	Default              any
	HasDefault           bool
	Items                *ConfigSchema
	AdditionalProperties bool
	Secret               bool
	// RestartOptional is x-terra-restart: false — recorded, not yet honoured.
	RestartOptional bool
}

// ParseConfigSchema checks a configuration schema against the subset and
// returns its parsed form. The root must be an object schema. x-terra-secret is
// accepted only on the root's own properties: masking works key by key, and a
// secret buried inside an object value would leave its parent readable.
func ParseConfigSchema(raw map[string]any) (*ConfigSchema, error) {
	if raw == nil {
		return nil, errors.New("configuration.schema is required")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("configuration.schema is not JSON: %w", err)
	}
	if len(encoded) > ConfigSchemaMaxBytes {
		return nil, fmt.Errorf("configuration.schema is %d bytes, over the %d byte limit", len(encoded), ConfigSchemaMaxBytes)
	}
	schema, err := parseConfigSchemaNode(raw, "configuration.schema", 0)
	if err != nil {
		return nil, err
	}
	if schema.Type != ConfigTypeObject {
		return nil, errors.New("configuration.schema must have type \"object\"")
	}
	return schema, nil
}

func parseConfigSchemaNode(raw map[string]any, path string, depth int) (*ConfigSchema, error) {
	for keyword := range raw {
		if !configSchemaKeywords[keyword] {
			return nil, fmt.Errorf("%s: keyword %q is outside the configuration schema subset", path, keyword)
		}
	}
	node := &ConfigSchema{AdditionalProperties: true}
	typeName, ok := raw["type"].(string)
	if !ok {
		return nil, fmt.Errorf("%s: type must be one string", path)
	}
	switch typeName {
	case ConfigTypeObject, ConfigTypeString, ConfigTypeInteger, ConfigTypeNumber, ConfigTypeBoolean, ConfigTypeArray:
	default:
		return nil, fmt.Errorf("%s: unsupported type %q", path, typeName)
	}
	node.Type = typeName
	if value, present := raw["title"]; present {
		if node.Title, ok = value.(string); !ok {
			return nil, fmt.Errorf("%s: title must be a string", path)
		}
	}
	if value, present := raw["description"]; present {
		if node.Description, ok = value.(string); !ok {
			return nil, fmt.Errorf("%s: description must be a string", path)
		}
	}
	if value, present := raw[ConfigSecretKeyword]; present {
		secret, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("%s: %s must be a boolean", path, ConfigSecretKeyword)
		}
		if secret && depth != 1 {
			return nil, fmt.Errorf("%s: %s is allowed only on top-level properties", path, ConfigSecretKeyword)
		}
		node.Secret = secret
	}
	if value, present := raw[ConfigRestartKeyword]; present {
		restart, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("%s: %s must be a boolean", path, ConfigRestartKeyword)
		}
		node.RestartOptional = !restart
	}

	only := func(keyword string, types ...string) error {
		if _, present := raw[keyword]; !present {
			return nil
		}
		for _, allowed := range types {
			if typeName == allowed {
				return nil
			}
		}
		return fmt.Errorf("%s: %s does not apply to type %q", path, keyword, typeName)
	}
	for keyword, types := range map[string][]string{
		"properties":           {ConfigTypeObject},
		"required":             {ConfigTypeObject},
		"additionalProperties": {ConfigTypeObject},
		"items":                {ConfigTypeArray},
		"minimum":              {ConfigTypeInteger, ConfigTypeNumber},
		"maximum":              {ConfigTypeInteger, ConfigTypeNumber},
		"minLength":            {ConfigTypeString},
		"maxLength":            {ConfigTypeString},
		"pattern":              {ConfigTypeString},
		"format":               {ConfigTypeString},
	} {
		if err := only(keyword, types...); err != nil {
			return nil, err
		}
	}

	if value, present := raw["properties"]; present {
		properties, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: properties must be an object", path)
		}
		node.Properties = make(map[string]*ConfigSchema, len(properties))
		for name, child := range properties {
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("%s: property names must not be empty", path)
			}
			childRaw, ok := child.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s.properties.%s must be a schema object", path, name)
			}
			parsed, err := parseConfigSchemaNode(childRaw, path+".properties."+name, depth+1)
			if err != nil {
				return nil, err
			}
			node.Properties[name] = parsed
		}
	}
	if value, present := raw["required"]; present {
		list, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("%s: required must be an array of property names", path)
		}
		for _, item := range list {
			name, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s: required must be an array of property names", path)
			}
			if _, declared := node.Properties[name]; !declared {
				return nil, fmt.Errorf("%s: required names undeclared property %q", path, name)
			}
			node.Required = append(node.Required, name)
		}
	}
	if value, present := raw["additionalProperties"]; present {
		allowed, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("%s: additionalProperties must be a boolean in the configuration schema subset", path)
		}
		node.AdditionalProperties = allowed
	}
	if value, present := raw["items"]; present {
		itemsRaw, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: items must be one schema object", path)
		}
		items, err := parseConfigSchemaNode(itemsRaw, path+".items", depth+1)
		if err != nil {
			return nil, err
		}
		node.Items = items
	}
	number := func(keyword string) (*float64, error) {
		value, present := raw[keyword]
		if !present {
			return nil, nil
		}
		parsed, ok := value.(float64)
		if !ok {
			return nil, fmt.Errorf("%s: %s must be a number", path, keyword)
		}
		return &parsed, nil
	}
	length := func(keyword string) (*int, error) {
		value, err := number(keyword)
		if err != nil || value == nil {
			return nil, err
		}
		if *value < 0 || *value != math.Trunc(*value) {
			return nil, fmt.Errorf("%s: %s must be a non-negative integer", path, keyword)
		}
		parsed := int(*value)
		return &parsed, nil
	}
	var err error
	if node.Minimum, err = number("minimum"); err != nil {
		return nil, err
	}
	if node.Maximum, err = number("maximum"); err != nil {
		return nil, err
	}
	if node.Minimum != nil && node.Maximum != nil && *node.Minimum > *node.Maximum {
		return nil, fmt.Errorf("%s: minimum is greater than maximum", path)
	}
	if node.MinLength, err = length("minLength"); err != nil {
		return nil, err
	}
	if node.MaxLength, err = length("maxLength"); err != nil {
		return nil, err
	}
	if value, present := raw["pattern"]; present {
		source, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s: pattern must be a string", path)
		}
		compiled, err := regexp.Compile(source)
		if err != nil {
			return nil, fmt.Errorf("%s: pattern does not compile: %v", path, err)
		}
		node.Pattern = compiled
	}
	if value, present := raw["format"]; present {
		format, ok := value.(string)
		if !ok || !configFormats[format] {
			return nil, fmt.Errorf("%s: format must be one of uri, hostname, ipv4", path)
		}
		node.Format = format
	}
	if value, present := raw["enum"]; present {
		list, ok := value.([]any)
		if !ok || len(list) == 0 {
			return nil, fmt.Errorf("%s: enum must be a non-empty array", path)
		}
		node.Enum = list
	}
	// default and enum members must themselves satisfy the node, or the schema
	// would hand a module a value it refuses on the next patch.
	for _, member := range node.Enum {
		if issues := node.validate(member, path); len(issues) > 0 {
			return nil, fmt.Errorf("%s: enum member does not satisfy the schema: %s", path, issues[0].Message)
		}
	}
	if value, present := raw["default"]; present {
		if issues := node.validate(value, path); len(issues) > 0 {
			return nil, fmt.Errorf("%s: default does not satisfy the schema: %s", path, issues[0].Message)
		}
		node.Default = value
		node.HasDefault = true
	}
	return node, nil
}

// ConfigKeyIssueCode classifies why one key was refused.
type ConfigKeyIssueCode string

// ConfigIssueType: the value is not of the declared type.
const ConfigIssueType ConfigKeyIssueCode = "TYPE"

// ConfigIssueValidation: right type, but a constraint (or required) fails.
const ConfigIssueValidation ConfigKeyIssueCode = "VALIDATION"

// ConfigIssueUnknown: the schema does not declare the key and forbids
// additional properties.
const ConfigIssueUnknown ConfigKeyIssueCode = "UNKNOWN"

// ConfigKeyIssue names one refused key. It never carries the value: a refused
// value is as likely to be a pasted secret as anything else, and this travels
// back over the wire and into logs.
type ConfigKeyIssue struct {
	Key     string             `json:"key"`
	Code    ConfigKeyIssueCode `json:"code"`
	Message string             `json:"message"`
}

// ConfigInvalidError is a refused set of values. Nothing was applied.
type ConfigInvalidError struct {
	Keys []ConfigKeyIssue
}

func (e *ConfigInvalidError) Error() string {
	if e == nil || len(e.Keys) == 0 {
		return "module configuration is invalid"
	}
	parts := make([]string, 0, len(e.Keys))
	for _, key := range e.Keys {
		parts = append(parts, fmt.Sprintf("%s: %s (%s)", key.Key, key.Message, key.Code))
	}
	return "module configuration is invalid: " + strings.Join(parts, "; ")
}

// SecretKeys lists the top-level properties marked x-terra-secret, sorted.
func (s *ConfigSchema) SecretKeys() []string {
	if s == nil {
		return nil
	}
	keys := []string{}
	for name, property := range s.Properties {
		if property.Secret {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

// Effective fills defaults into a copy of values and validates the result. It
// returns the effective values a module starts with, or a *ConfigInvalidError
// naming every refused key. values is never changed.
func (s *ConfigSchema) Effective(values map[string]any) (map[string]any, error) {
	effective, _ := s.withDefaults(cloneConfigValue(values)).(map[string]any)
	if effective == nil {
		effective = map[string]any{}
	}
	if issues := s.validate(effective, ""); len(issues) > 0 {
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Key < issues[j].Key })
		return nil, &ConfigInvalidError{Keys: issues}
	}
	return effective, nil
}

func (s *ConfigSchema) withDefaults(value any) any {
	if s == nil || s.Type != ConfigTypeObject {
		return value
	}
	object, ok := value.(map[string]any)
	if value == nil {
		object, ok = map[string]any{}, true
	}
	if !ok {
		return value
	}
	for name, property := range s.Properties {
		current, present := object[name]
		if !present && property.HasDefault {
			object[name] = cloneConfigValue(property.Default)
			continue
		}
		if present && property.Type == ConfigTypeObject {
			object[name] = property.withDefaults(current)
		}
	}
	return object
}

func joinConfigKey(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func (s *ConfigSchema) validate(value any, key string) []ConfigKeyIssue {
	issue := func(code ConfigKeyIssueCode, format string, args ...any) []ConfigKeyIssue {
		return []ConfigKeyIssue{{Key: key, Code: code, Message: fmt.Sprintf(format, args...)}}
	}
	switch s.Type {
	case ConfigTypeObject:
		object, ok := value.(map[string]any)
		if !ok {
			return issue(ConfigIssueType, "must be an object")
		}
		var issues []ConfigKeyIssue
		for _, name := range s.Required {
			if _, present := object[name]; !present {
				issues = append(issues, ConfigKeyIssue{Key: joinConfigKey(key, name), Code: ConfigIssueValidation, Message: "is required"})
			}
		}
		names := make([]string, 0, len(object))
		for name := range object {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			property, declared := s.Properties[name]
			if !declared {
				if !s.AdditionalProperties {
					issues = append(issues, ConfigKeyIssue{Key: joinConfigKey(key, name), Code: ConfigIssueUnknown, Message: "is not a configuration key of this module"})
				}
				continue
			}
			issues = append(issues, property.validate(object[name], joinConfigKey(key, name))...)
		}
		return issues
	case ConfigTypeArray:
		list, ok := value.([]any)
		if !ok {
			return issue(ConfigIssueType, "must be an array")
		}
		var issues []ConfigKeyIssue
		if s.Items != nil {
			for index, item := range list {
				issues = append(issues, s.Items.validate(item, fmt.Sprintf("%s[%d]", key, index))...)
			}
		}
		return append(issues, s.validateEnum(value, key)...)
	case ConfigTypeBoolean:
		if _, ok := value.(bool); !ok {
			return issue(ConfigIssueType, "must be a boolean")
		}
		return s.validateEnum(value, key)
	case ConfigTypeInteger, ConfigTypeNumber:
		number, ok := value.(float64)
		if !ok {
			return issue(ConfigIssueType, "must be a %s", s.Type)
		}
		if s.Type == ConfigTypeInteger && number != math.Trunc(number) {
			return issue(ConfigIssueType, "must be an integer")
		}
		if s.Minimum != nil && number < *s.Minimum {
			return issue(ConfigIssueValidation, "must be at least %v", *s.Minimum)
		}
		if s.Maximum != nil && number > *s.Maximum {
			return issue(ConfigIssueValidation, "must be at most %v", *s.Maximum)
		}
		return s.validateEnum(value, key)
	case ConfigTypeString:
		text, ok := value.(string)
		if !ok {
			return issue(ConfigIssueType, "must be a string")
		}
		length := len([]rune(text))
		if s.MinLength != nil && length < *s.MinLength {
			return issue(ConfigIssueValidation, "must be at least %d characters", *s.MinLength)
		}
		if s.MaxLength != nil && length > *s.MaxLength {
			return issue(ConfigIssueValidation, "must be at most %d characters", *s.MaxLength)
		}
		if s.Pattern != nil && !s.Pattern.MatchString(text) {
			return issue(ConfigIssueValidation, "does not match the declared pattern")
		}
		if s.Format != "" && !validConfigFormat(s.Format, text) {
			return issue(ConfigIssueValidation, "is not a valid %s", s.Format)
		}
		return s.validateEnum(value, key)
	}
	return issue(ConfigIssueType, "has an unsupported schema type")
}

func (s *ConfigSchema) validateEnum(value any, key string) []ConfigKeyIssue {
	if len(s.Enum) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(value)
	for _, member := range s.Enum {
		candidate, _ := json.Marshal(member)
		if string(candidate) == string(encoded) {
			return nil
		}
	}
	return []ConfigKeyIssue{{Key: key, Code: ConfigIssueValidation, Message: "must be one of the declared values"}}
}

func validConfigFormat(format, text string) bool {
	switch format {
	case "uri":
		parsed, err := url.Parse(text)
		return err == nil && parsed.Scheme != "" && (parsed.Host != "" || parsed.Opaque != "" || parsed.Path != "")
	case "hostname":
		return len(text) <= 253 && hostnamePattern.MatchString(text)
	case "ipv4":
		ip := net.ParseIP(text)
		return ip != nil && ip.To4() != nil && strings.Count(text, ".") == 3
	}
	return false
}

// cloneConfigValue deep-copies decoded JSON so a caller's map is never shared.
func cloneConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneConfigValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = cloneConfigValue(item)
		}
		return out
	}
	return value
}

// CloneConfigValue deep-copies decoded JSON (maps, slices and scalars) so a
// caller's value is never shared with the schema's defaults or a store.
func CloneConfigValue(value any) any { return cloneConfigValue(value) }
