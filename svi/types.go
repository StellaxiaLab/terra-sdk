package svi

import "time"

type SubjectType string

const (
	SubjectUser           SubjectType = "user"
	SubjectOrganization   SubjectType = "organization"
	SubjectNode           SubjectType = "node"
	SubjectApplication    SubjectType = "application"
	SubjectServiceAccount SubjectType = "service-account"
	SubjectSystem         SubjectType = "system"
)

type SubjectRef struct {
	Type SubjectType `json:"type"`
	ID   string      `json:"id"`
}

type ResourceStatus string

const (
	ResourceAvailable   ResourceStatus = "available"
	ResourceBusy        ResourceStatus = "busy"
	ResourceDisabled    ResourceStatus = "disabled"
	ResourceUnavailable ResourceStatus = "unavailable"
	ResourceUnsupported ResourceStatus = "unsupported"
)

type Direction string

const (
	DirectionSource Direction = "source"
	DirectionSink   Direction = "sink"
	DirectionDuplex Direction = "duplex"
)

type Interaction string

const (
	InteractionSnapshot   Interaction = "snapshot"
	InteractionStream     Interaction = "stream"
	InteractionCollection Interaction = "collection"
	InteractionBlob       Interaction = "blob"
	InteractionCommand    Interaction = "command"
	InteractionDuplex     Interaction = "duplex"
)

type Operation string

const (
	OperationDiscover   Operation = "discover"
	OperationInspect    Operation = "inspect"
	OperationRead       Operation = "read"
	OperationWrite      Operation = "write"
	OperationList       Operation = "list"
	OperationWatch      Operation = "watch"
	OperationSubscribe  Operation = "subscribe"
	OperationInvoke     Operation = "invoke"
	OperationBindSource Operation = "bind.source"
	OperationBindTarget Operation = "bind.target"
)

type QoSProfile string

const (
	QoSRealtimeLatest  QoSProfile = "realtime_latest"
	QoSRealtimeOrdered QoSProfile = "realtime_ordered"
	QoSReliableOrdered QoSProfile = "reliable_ordered"
	QoSBulkResumable   QoSProfile = "bulk_resumable"
)

type EndpointDescriptor struct {
	EndpointID   string       `json:"endpoint_id"`
	Direction    Direction    `json:"direction"`
	Interaction  Interaction  `json:"interaction"`
	Operations   []Operation  `json:"operations"`
	InputSchema  string       `json:"input_schema,omitempty"`
	OutputSchema string       `json:"output_schema,omitempty"`
	Encodings    []string     `json:"encodings,omitempty"`
	QoSProfiles  []QoSProfile `json:"qos_profiles,omitempty"`
	Sensitivity  string       `json:"sensitivity,omitempty"`
	Exclusive    bool         `json:"exclusive,omitempty"`
	// Resumable reports whether this endpoint can produce again from a given
	// sequence. A file can; a process cannot, because re-running it is a new
	// execution rather than a resume. The zero value is false, which is why
	// every descriptor written before this field stays correct.
	Resumable    bool           `json:"resumable,omitempty"`
	MaxConsumers int            `json:"max_consumers,omitempty"`
	Status       ResourceStatus `json:"status"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type ResourceDescriptor struct {
	ResourceID    string               `json:"resource_id"`
	NodeID        string               `json:"node_id"`
	Kind          string               `json:"kind"`
	CanonicalName string               `json:"canonical_name"`
	DisplayName   string               `json:"display_name,omitempty"`
	ProviderID    string               `json:"provider_id"`
	Owner         SubjectRef           `json:"owner"`
	Labels        map[string]string    `json:"labels,omitempty"`
	Generation    uint64               `json:"generation"`
	Revision      uint64               `json:"revision"`
	Status        ResourceStatus       `json:"status"`
	ExpiresAt     time.Time            `json:"expires_at"`
	Endpoints     []EndpointDescriptor `json:"endpoints"`
}

type HandleRequest struct {
	ResourceID        string     `json:"resource_id"`
	EndpointID        string     `json:"endpoint_id"`
	Operation         Operation  `json:"operation"`
	RequestedSchema   string     `json:"requested_schema,omitempty"`
	RequestedEncoding string     `json:"requested_encoding,omitempty"`
	QoSProfile        QoSProfile `json:"qos_profile,omitempty"`
	IdempotencyKey    string     `json:"idempotency_key"`
	TTLSeconds        int        `json:"ttl_seconds,omitempty"`
}

type EndpointReference struct {
	ResourceID string `json:"resource_id"`
	EndpointID string `json:"endpoint_id"`
}

type BindingDesiredState string

const (
	BindingDesiredActive BindingDesiredState = "active"
	BindingDesiredClosed BindingDesiredState = "closed"
)

type CompatibilityPolicy string

const (
	CompatibilityPolicyExact           CompatibilityPolicy = "exact"
	CompatibilityPolicyAllowCompatible CompatibilityPolicy = "allow_compatible"
	CompatibilityPolicyAllowTransform  CompatibilityPolicy = "allow_transform"
)

type BindingDeclaration struct {
	BindingID           string              `json:"binding_id,omitempty"`
	Source              EndpointReference   `json:"source"`
	Target              EndpointReference   `json:"target"`
	DesiredState        BindingDesiredState `json:"desired_state"`
	CompatibilityPolicy CompatibilityPolicy `json:"compatibility_policy"`
	QoSProfile          QoSProfile          `json:"qos_profile"`
	IdempotencyKey      string              `json:"idempotency_key"`
	Generation          uint64              `json:"generation,omitempty"`
}

type RuntimeKind string

const (
	RuntimeHandle     RuntimeKind = "handle"
	RuntimeBinding    RuntimeKind = "binding"
	RuntimeProjection RuntimeKind = "projection"
)

type RuntimeState string

const (
	RuntimeRequested  RuntimeState = "requested"
	RuntimeAuthorized RuntimeState = "authorized"
	RuntimeValidating RuntimeState = "validating"
	RuntimePlanning   RuntimeState = "planning"
	RuntimePreparing  RuntimeState = "preparing"
	RuntimeOpening    RuntimeState = "opening"
	RuntimeActive     RuntimeState = "active"
	RuntimeDegraded   RuntimeState = "degraded"
	RuntimeClosing    RuntimeState = "closing"
	RuntimeClosed     RuntimeState = "closed"
	RuntimeDenied     RuntimeState = "denied"
	RuntimeFailed     RuntimeState = "failed"
	RuntimeExpired    RuntimeState = "expired"
)

type RuntimeStatus struct {
	RuntimeID    string       `json:"runtime_id"`
	Kind         RuntimeKind  `json:"kind"`
	ResourceID   string       `json:"resource_id,omitempty"`
	HandleID     string       `json:"handle_id,omitempty"`
	BindingID    string       `json:"binding_id,omitempty"`
	ProjectionID string       `json:"projection_id,omitempty"`
	State        RuntimeState `json:"state"`
	Reason       string       `json:"reason,omitempty"`
	Generation   uint64       `json:"generation"`
	PlanRevision uint64       `json:"plan_revision"`
	UpdatedAt    time.Time    `json:"updated_at"`
	TraceID      string       `json:"trace_id,omitempty"`
}
