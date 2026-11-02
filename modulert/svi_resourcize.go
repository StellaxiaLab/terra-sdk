package modulert

import (
	"encoding/json"
	"time"
)

// The host↔module resourcizer seam (Terra resourcizer framework design §4–§5).
//
// A module that can turn something into an SVI resource on request — an RTSP
// camera given its address, a program window given its name — says so in two
// halves, both host→module and both guarded by the workload credential:
//
//   - SVIResourcizersPath (GET): the TYPES it offers, each with the config
//     schema a controller draws the create form from. The module owns the
//     list; the host only reads it, so a stopped module leaves no orphans.
//   - SVIInstancesPath (PUT): the INSTANCES the node declared for this module.
//     The host owns declarations (they survive the module); it hands the
//     module the whole desired set, level-triggered, after every change and
//     after every module start. The module stores nothing and answers what it
//     made of each one.
//
// The resources an instance realizes are published on SVIResourcesPath like
// any other, and data reaches a sink instance on SVISinkPath. The manifest
// opts in with contributions.svi.resourcizers.
const (
	SVIResourcizersPath      = "/terra/svi/resourcizers"
	SVIInstancesPath         = "/terra/svi/instances"
	SVIInstancesValidatePath = "/terra/svi/instances/validate"
)

// ResourcizerList is the module's answer on SVIResourcizersPath. ETag changes
// exactly when the types do.
type ResourcizerList struct {
	ETag  string            `json:"etag"`
	Types []ResourcizerType `json:"types"`
}

// ResourcizerType is one kind of thing the module turns into a resource.
type ResourcizerType struct {
	// ID is unique within the module and stable: [a-z0-9]+([.-][a-z0-9]+)*.
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Icon is a path under the module's ui/ directory, never a URL.
	Icon string `json:"icon,omitempty"`
	// ConfigSchema is the drawable JSON Schema subset (svi.CheckConfigSchema).
	ConfigSchema map[string]any        `json:"configSchema"`
	Endpoints    []ResourcizerEndpoint `json:"endpoints"`
	Accepts      []ResourcizerAccept   `json:"accepts,omitempty"`
	MaxInstances int                   `json:"maxInstances,omitempty"`
	Labels       map[string]string     `json:"labels,omitempty"`
}

// ResourcizerEndpoint is the shape an instance's endpoint takes. When names the
// config values that select it; empty means every instance has it.
type ResourcizerEndpoint struct {
	ID         string            `json:"id"`
	Direction  string            `json:"direction"`
	Schema     string            `json:"schema"`
	Operations []string          `json:"operations"`
	When       map[string]string `json:"when,omitempty"`
}

// ResourcizerAccept is a reference a create form can start from: a right click
// on a file (Ref "file") prefills Field. Types narrows file references by MIME
// pattern or extension. A reference prefills a form; it grants nothing.
type ResourcizerAccept struct {
	Ref   string   `json:"ref"`
	Field string   `json:"field"`
	Types []string `json:"types,omitempty"`
}

// InstanceSpec is one declared instance as the host hands it over.
type InstanceSpec struct {
	// Name is unique on the node: [a-z0-9]([a-z0-9-]*[a-z0-9])?.
	Name string `json:"name"`
	// Type is the module-local type id.
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

// InstanceSet is the whole desired set for this module (PUT SVIInstancesPath).
type InstanceSet struct {
	Instances []InstanceSpec `json:"instances"`
}

// Instance states a module reports.
const (
	InstanceStarting = "starting"
	InstanceReady    = "ready"
	InstanceFailed   = "failed"
)

// InstanceStatus is what the module made of one instance.
type InstanceStatus struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	// Resources are the module-local resource suffixes the instance publishes.
	Resources []string `json:"resources,omitempty"`
	// UpdatedAt is when State last changed — not when the instance was
	// declared, so a status that moves is visibly moving.
	UpdatedAt time.Time `json:"updatedAt"`
}

// InstanceSetResult answers a PUT with every instance's status.
type InstanceSetResult struct {
	Instances []InstanceStatus `json:"instances"`
}

// FieldError points at one bad config value. Path is a JSON Pointer into the
// config ("/url"); empty means the config as a whole.
type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// InstanceValidateResponse answers POST SVIInstancesValidatePath: 200 with no
// errors, 422 with them, 404 for an unknown type.
type InstanceValidateResponse struct {
	Errors []FieldError `json:"errors,omitempty"`
	Detail string       `json:"detail,omitempty"`
}
