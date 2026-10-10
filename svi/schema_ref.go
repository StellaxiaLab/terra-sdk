package svi

import (
	"fmt"
	"regexp"
	"strconv"
)

var schemaRefPattern = regexp.MustCompile(`^terra\.([a-z0-9]+(?:[.-][a-z0-9]+)*)@([1-9][0-9]*)$`)

type SchemaRef struct {
	Raw   string
	Name  string
	Major int
}

func ParseSchemaRef(value string) (SchemaRef, error) {
	match := schemaRefPattern.FindStringSubmatch(value)
	if len(match) != 3 {
		return SchemaRef{}, fmt.Errorf("%w: invalid schema_ref %q", ErrInvalidSchemaRef, value)
	}
	major, err := strconv.Atoi(match[2])
	if err != nil {
		return SchemaRef{}, fmt.Errorf("%w: invalid major version: %v", ErrInvalidSchemaRef, err)
	}
	return SchemaRef{Raw: value, Name: match[1], Major: major}, nil
}
