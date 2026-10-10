package svi

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Validation of the descriptors a module publishes. These are the checks the
// host runs on a ResourceDescriptor before it accepts it, copied from Terra's
// terra-svi unchanged so a module can run the same checks in its own tests.

var (
	ErrInvalidResource  = errors.New("invalid SVI resource")
	ErrInvalidEndpoint  = errors.New("invalid SVI endpoint")
	ErrInvalidSchemaRef = errors.New("invalid SVI schema reference")
)

var kindPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

func ValidateResource(resource ResourceDescriptor) error {
	if blank(resource.ResourceID) || blank(resource.NodeID) || blank(resource.CanonicalName) || blank(resource.ProviderID) {
		return fmt.Errorf("%w: resource_id, node_id, canonical_name, and provider_id are required", ErrInvalidResource)
	}
	if !kindPattern.MatchString(resource.Kind) {
		return fmt.Errorf("%w: invalid kind %q", ErrInvalidResource, resource.Kind)
	}
	if !validSubjectType(resource.Owner.Type) || blank(resource.Owner.ID) {
		return fmt.Errorf("%w: valid owner type and id are required", ErrInvalidResource)
	}
	if !validResourceStatus(resource.Status) {
		return fmt.Errorf("%w: invalid status %q", ErrInvalidResource, resource.Status)
	}
	if resource.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: expires_at is required", ErrInvalidResource)
	}
	if len(resource.Endpoints) == 0 {
		return fmt.Errorf("%w: at least one endpoint is required", ErrInvalidResource)
	}
	endpoints := make(map[string]struct{}, len(resource.Endpoints))
	for index, endpoint := range resource.Endpoints {
		if err := ValidateEndpoint(endpoint); err != nil {
			return fmt.Errorf("%w: endpoint[%d]: %v", ErrInvalidResource, index, err)
		}
		if _, exists := endpoints[endpoint.EndpointID]; exists {
			return fmt.Errorf("%w: duplicate endpoint_id %q", ErrInvalidResource, endpoint.EndpointID)
		}
		endpoints[endpoint.EndpointID] = struct{}{}
	}
	return nil
}

func ValidateEndpoint(endpoint EndpointDescriptor) error {
	if blank(endpoint.EndpointID) {
		return fmt.Errorf("%w: endpoint_id is required", ErrInvalidEndpoint)
	}
	if !validDirection(endpoint.Direction) {
		return fmt.Errorf("%w: invalid direction %q", ErrInvalidEndpoint, endpoint.Direction)
	}
	if !validInteraction(endpoint.Interaction) {
		return fmt.Errorf("%w: invalid interaction %q", ErrInvalidEndpoint, endpoint.Interaction)
	}
	if !validResourceStatus(endpoint.Status) {
		return fmt.Errorf("%w: invalid status %q", ErrInvalidEndpoint, endpoint.Status)
	}
	if len(endpoint.Operations) == 0 {
		return fmt.Errorf("%w: at least one operation is required", ErrInvalidEndpoint)
	}
	operations := make(map[Operation]struct{}, len(endpoint.Operations))
	for _, operation := range endpoint.Operations {
		if !validOperation(operation) {
			return fmt.Errorf("%w: invalid operation %q", ErrInvalidEndpoint, operation)
		}
		if _, exists := operations[operation]; exists {
			return fmt.Errorf("%w: duplicate operation %q", ErrInvalidEndpoint, operation)
		}
		operations[operation] = struct{}{}
	}
	if endpoint.MaxConsumers < 0 {
		return fmt.Errorf("%w: max_consumers cannot be negative", ErrInvalidEndpoint)
	}
	switch endpoint.Direction {
	case DirectionSource:
		if blank(endpoint.OutputSchema) {
			return fmt.Errorf("%w: source endpoint requires output_schema", ErrInvalidEndpoint)
		}
	case DirectionSink:
		if blank(endpoint.InputSchema) {
			return fmt.Errorf("%w: sink endpoint requires input_schema", ErrInvalidEndpoint)
		}
	case DirectionDuplex:
		if blank(endpoint.InputSchema) && blank(endpoint.OutputSchema) {
			return fmt.Errorf("%w: duplex endpoint requires input_schema or output_schema", ErrInvalidEndpoint)
		}
	}
	if !blank(endpoint.InputSchema) {
		if _, err := ParseSchemaRef(endpoint.InputSchema); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
		}
	}
	if !blank(endpoint.OutputSchema) {
		if _, err := ParseSchemaRef(endpoint.OutputSchema); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
		}
	}
	if err := validateUniqueStrings("encoding", endpoint.Encodings); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
	}
	qos := make(map[QoSProfile]struct{}, len(endpoint.QoSProfiles))
	for _, profile := range endpoint.QoSProfiles {
		if !validQoSProfile(profile) {
			return fmt.Errorf("%w: invalid qos profile %q", ErrInvalidEndpoint, profile)
		}
		if _, exists := qos[profile]; exists {
			return fmt.Errorf("%w: duplicate qos profile %q", ErrInvalidEndpoint, profile)
		}
		qos[profile] = struct{}{}
	}
	for key, value := range endpoint.Metadata {
		if blank(key) || !validMetadataValue(value) {
			return fmt.Errorf("%w: metadata %q must contain a scalar string, number, or boolean", ErrInvalidEndpoint, key)
		}
	}
	return nil
}

func validateUniqueStrings(name string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if blank(value) {
			return fmt.Errorf("%s cannot be empty", name)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate %s %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func validSubjectType(value SubjectType) bool {
	switch value {
	case SubjectUser, SubjectOrganization, SubjectNode, SubjectApplication, SubjectServiceAccount, SubjectSystem:
		return true
	default:
		return false
	}
}

func validResourceStatus(value ResourceStatus) bool {
	switch value {
	case ResourceAvailable, ResourceBusy, ResourceDisabled, ResourceUnavailable, ResourceUnsupported:
		return true
	default:
		return false
	}
}

func validDirection(value Direction) bool {
	return value == DirectionSource || value == DirectionSink || value == DirectionDuplex
}

func validInteraction(value Interaction) bool {
	switch value {
	case InteractionSnapshot, InteractionStream, InteractionCollection, InteractionBlob, InteractionCommand, InteractionDuplex:
		return true
	default:
		return false
	}
}

func validOperation(value Operation) bool {
	switch value {
	case OperationDiscover, OperationInspect, OperationRead, OperationWrite, OperationList, OperationWatch, OperationSubscribe, OperationInvoke, OperationBindSource, OperationBindTarget:
		return true
	default:
		return false
	}
}

func validQoSProfile(value QoSProfile) bool {
	switch value {
	case QoSRealtimeLatest, QoSRealtimeOrdered, QoSReliableOrdered, QoSBulkResumable:
		return true
	default:
		return false
	}
}

func validMetadataValue(value any) bool {
	switch value.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, json.Number:
		return true
	default:
		return false
	}
}
