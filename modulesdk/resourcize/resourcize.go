// Package resourcize is the framework a module uses to turn things into SVI
// resources on request (Terra resourcizer framework design §6).
//
// A module writes a type — its id, a config schema, the endpoints an instance
// has, and an Open function — and Install wires the rest into
// modulesdk.Config: the type list the controller draws its create form from,
// the instance set the node hands over, the descriptors the daemon polls, and
// the sink seam bindings write to. What the framework owns so a module cannot
// get it wrong:
//
//   - the type list and its etag, and the schema subset check at Register
//   - level-triggered instances: the same set twice changes nothing; a removed
//     instance closes its open streams with resource_withdrawn, a changed one
//     with resource_replaced
//   - one state per instance that both the descriptor status and the open
//     decision read, so a resource the catalog calls unavailable cannot be
//     opened (the 9/9 audit's "list and execution disagree")
//   - 404 for "not mine", 503 with the reason for "mine but not now", and a
//     close that takes effect once and answers success after
//   - status times that move when the state does, not when it was declared
//
// The module stores nothing: the node owns declarations and hands the whole
// set back after every module start.
package resourcize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
	"github.com/StellaxiaLab/terra-sdk/modulesdk"
	coresvi "github.com/StellaxiaLab/terra-sdk/svi"
)

// Directions an endpoint takes.
const (
	Source = "source"
	Sink   = "sink"
)

// Reasons the framework closes a stream with.
const (
	ReasonWithdrawn = "resource_withdrawn"
	ReasonReplaced  = "resource_replaced"
)

// descriptorTTL is how long the daemon may keep a descriptor without a fresh
// poll. The daemon stamps its own; this keeps the descriptor valid on its own.
const descriptorTTL = 2 * time.Minute

var (
	typeIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	namePattern   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	// The daemon's built-in families own these suffix segments.
	reservedTypePrefixes = []string{"process.", "fs.", "test.", "net.", "svi."}
)

// Endpoint is one endpoint an instance of a type has.
type Endpoint struct {
	ID        string
	Direction string
	// Schema is the frame schema ("terra.bytes@1").
	Schema string
	// Operations default by direction: source inspect·read·subscribe·
	// bind.source, sink write·bind.target.
	Operations   []string
	When         map[string]string
	QoSProfiles  []coresvi.QoSProfile
	Encodings    []string
	Sensitivity  string
	MaxConsumers int
	Exclusive    bool
}

// Instance is one declared instance with its decoded config.
type Instance[C any] struct {
	Name   string
	Type   string
	Config C
}

// StreamSink absorbs one binding's frames for an instance endpoint. Frames
// arrive ordered and without gaps. Close is called exactly once.
type StreamSink interface {
	Write(ctx context.Context, sequence uint64, data []byte) error
	Close(reason string) error
}

// StreamSource produces frames for an instance endpoint (the source seam,
// design §7). Next returns the next frame, io.EOF when the stream ended on its
// own, or the failure. Close must unblock a Next in flight.
type StreamSource interface {
	Next(ctx context.Context) ([]byte, error)
	Close(reason string) error
}

// Declared is a type users create instances of by filling its form.
type Declared[C any] struct {
	ID          string
	Title       string
	Description string
	Icon        string
	// Kind is the resource kind instances publish; default ID.
	Kind string
	// Schema is the config form (the drawable subset, svi.CheckConfigSchema).
	Schema       map[string]any
	Endpoints    []Endpoint
	Accepts      []modulert.ResourcizerAccept
	MaxInstances int
	// Validate checks meaning the schema cannot (optional). Return
	// FieldErrors to point at values.
	Validate func(ctx context.Context, instance Instance[C]) error
	// Realize brings an instance up — connect, probe — and runs off the
	// request path. nil means ready at once. An error fails the instance with
	// the error as its reason; Unavailable gives a short reason code.
	Realize func(ctx context.Context, instance Instance[C]) error
	// Release undoes Realize when the instance goes away (optional).
	Release func(instance Instance[C])
	// OpenSink opens a sink on one of the instance's sink endpoints.
	OpenSink func(ctx context.Context, instance Instance[C], endpointID string) (StreamSink, error)
	// OpenSource opens a source on one of the instance's source endpoints.
	// A type without it publishes sources nobody can bind from, so Register
	// refuses that.
	OpenSource func(ctx context.Context, instance Instance[C], endpointID string) (StreamSource, error)
}

// FieldErrors points at bad config values; return it from Validate.
type FieldErrors []modulert.FieldError

func (f FieldErrors) Error() string {
	parts := make([]string, 0, len(f))
	for _, err := range f {
		parts = append(parts, strings.TrimSpace(err.Path+" "+err.Message))
	}
	return strings.Join(parts, "; ")
}

// Field is one FieldError.
func Field(path, message string) FieldErrors {
	return FieldErrors{{Path: path, Message: message}}
}

type unavailableError struct{ reason string }

func (e unavailableError) Error() string { return e.reason }

// Unavailable is an error whose text is a short reason code
// ("rtsp_connect_refused"): the instance or open failed for a reason the
// controller shows as is.
func Unavailable(reason string) error { return unavailableError{reason: reason} }

func reasonOf(err error) string {
	var unavailable unavailableError
	if errors.As(err, &unavailable) {
		return unavailable.reason
	}
	text := err.Error()
	if len(text) > 256 {
		text = text[:256]
	}
	return text
}

// entry erases a Declared's config type.
type entry interface {
	spec() modulert.ResourcizerType
	kind() string
	endpoints() []Endpoint
	decode(raw json.RawMessage) (any, error)
	validate(ctx context.Context, name string, config any) error
	realize(ctx context.Context, name string, config any) error
	release(name string, config any)
	openSink(ctx context.Context, name string, config any, endpointID string) (StreamSink, error)
	openSource(ctx context.Context, name string, config any, endpointID string) (StreamSource, error)
	maxInstances() int
}

type declared[C any] struct{ d Declared[C] }

func (e declared[C]) spec() modulert.ResourcizerType {
	endpoints := make([]modulert.ResourcizerEndpoint, 0, len(e.d.Endpoints))
	for _, endpoint := range e.d.Endpoints {
		endpoints = append(endpoints, modulert.ResourcizerEndpoint{
			ID: endpoint.ID, Direction: endpoint.Direction, Schema: endpoint.Schema,
			Operations: operationsOf(endpoint), When: endpoint.When,
		})
	}
	return modulert.ResourcizerType{
		ID: e.d.ID, Title: e.d.Title, Description: e.d.Description, Icon: e.d.Icon,
		ConfigSchema: e.d.Schema, Endpoints: endpoints, Accepts: e.d.Accepts, MaxInstances: e.d.MaxInstances,
	}
}

func (e declared[C]) kind() string {
	if e.d.Kind != "" {
		return e.d.Kind
	}
	return e.d.ID
}

func (e declared[C]) endpoints() []Endpoint { return e.d.Endpoints }
func (e declared[C]) maxInstances() int     { return e.d.MaxInstances }

func (e declared[C]) decode(raw json.RawMessage) (any, error) {
	var config C
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func (e declared[C]) instance(name string, config any) Instance[C] {
	return Instance[C]{Name: name, Type: e.d.ID, Config: config.(C)}
}

func (e declared[C]) validate(ctx context.Context, name string, config any) error {
	if e.d.Validate == nil {
		return nil
	}
	return e.d.Validate(ctx, e.instance(name, config))
}

func (e declared[C]) realize(ctx context.Context, name string, config any) error {
	if e.d.Realize == nil {
		return nil
	}
	return e.d.Realize(ctx, e.instance(name, config))
}

func (e declared[C]) release(name string, config any) {
	if e.d.Release != nil {
		e.d.Release(e.instance(name, config))
	}
}

func (e declared[C]) openSink(ctx context.Context, name string, config any, endpointID string) (StreamSink, error) {
	if e.d.OpenSink == nil {
		return nil, modulesdk.ErrSVISinkNotServed
	}
	return e.d.OpenSink(ctx, e.instance(name, config), endpointID)
}

func (e declared[C]) openSource(ctx context.Context, name string, config any, endpointID string) (StreamSource, error) {
	if e.d.OpenSource == nil {
		return nil, modulesdk.ErrSVISourceNotServed
	}
	return e.d.OpenSource(ctx, e.instance(name, config), endpointID)
}

func operationsOf(endpoint Endpoint) []string {
	if len(endpoint.Operations) > 0 {
		return endpoint.Operations
	}
	if endpoint.Direction == Sink {
		return []string{string(coresvi.OperationWrite), string(coresvi.OperationBindTarget)}
	}
	return []string{string(coresvi.OperationInspect), string(coresvi.OperationRead), string(coresvi.OperationSubscribe), string(coresvi.OperationBindSource)}
}

// Framework holds a module's types and instances.
type Framework struct {
	moduleID string
	now      func() time.Time

	mu        sync.Mutex
	types     map[string]entry
	order     []string
	instances map[string]*instance
}

// instance is one live instance. Each apply that changes it makes a new one,
// so a Realize finishing late can tell it has been replaced.
type instance struct {
	spec      modulert.InstanceSpec
	canonical string
	entry     entry
	config    any
	values    map[string]string
	state     string
	reason    string
	updatedAt time.Time
	cancel    context.CancelFunc
	streams   map[ender]struct{}
}

// New starts an empty framework for the module.
func New(moduleID string) *Framework {
	return &Framework{
		moduleID: moduleID, now: func() time.Time { return time.Now().UTC() },
		types: map[string]entry{}, instances: map[string]*instance{},
	}
}

// Register adds a type. It refuses a type whose id, schema or endpoints the
// node would refuse, so a bad type fails when the module starts rather than
// when someone first tries to create one.
func Register[C any](f *Framework, d Declared[C]) error {
	if !typeIDPattern.MatchString(d.ID) {
		return fmt.Errorf("resourcize: type id %q must be lowercase segments joined by . or -", d.ID)
	}
	for _, prefix := range reservedTypePrefixes {
		if strings.HasPrefix(d.ID+".", prefix) {
			return fmt.Errorf("resourcize: type id %q uses the reserved %q segment of a built-in family", d.ID, strings.TrimSuffix(prefix, "."))
		}
	}
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("resourcize: type %s needs a title", d.ID)
	}
	if err := coresvi.CheckConfigSchema(roundTrip(d.Schema)); err != nil {
		return fmt.Errorf("resourcize: type %s: %w", d.ID, err)
	}
	if len(d.Endpoints) == 0 {
		return fmt.Errorf("resourcize: type %s needs at least one endpoint", d.ID)
	}
	properties, _ := roundTrip(d.Schema)["properties"].(map[string]any)
	seen := map[string]bool{}
	for _, endpoint := range d.Endpoints {
		if endpoint.ID == "" || seen[endpoint.ID+"/"+fmt.Sprint(endpoint.When)] {
			return fmt.Errorf("resourcize: type %s: endpoint ids must be present and distinct", d.ID)
		}
		seen[endpoint.ID+"/"+fmt.Sprint(endpoint.When)] = true
		if endpoint.Direction != Source && endpoint.Direction != Sink {
			return fmt.Errorf("resourcize: type %s endpoint %s: direction must be source or sink", d.ID, endpoint.ID)
		}
		if _, err := coresvi.ParseSchemaRef(endpoint.Schema); err != nil {
			return fmt.Errorf("resourcize: type %s endpoint %s: %w", d.ID, endpoint.ID, err)
		}
		if endpoint.Direction == Sink && d.OpenSink == nil {
			return fmt.Errorf("resourcize: type %s has sink endpoint %s but no OpenSink", d.ID, endpoint.ID)
		}
		if endpoint.Direction == Source && d.OpenSource == nil {
			return fmt.Errorf("resourcize: type %s has source endpoint %s but no OpenSource", d.ID, endpoint.ID)
		}
		for field := range endpoint.When {
			if _, ok := properties[field]; !ok {
				return fmt.Errorf("resourcize: type %s endpoint %s: when names %q, which is not a config field", d.ID, endpoint.ID, field)
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.types[d.ID]; exists {
		return fmt.Errorf("resourcize: type %s registered twice", d.ID)
	}
	f.types[d.ID] = declared[C]{d: d}
	f.order = append(f.order, d.ID)
	return nil
}

func roundTrip(schema map[string]any) map[string]any {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var decoded map[string]any
	_ = json.Unmarshal(encoded, &decoded)
	return decoded
}

// Install wires the framework into a module config: the resourcizer seam,
// the descriptor poll and the sink seam. A config that already serves its own
// resources or sink is refused — two owners of one seam would each tombstone
// the other's resources.
func (f *Framework) Install(cfg *modulesdk.Config) error {
	if cfg.SVIResourcizer != nil || cfg.SVIResources != nil || cfg.SVISink != nil || cfg.SVISource != nil {
		return errors.New("resourcize: the config already serves SVI resources, sink, source or resourcizers")
	}
	cfg.SVIResourcizer = f
	cfg.SVIResources = f.Resources
	cfg.SVISink = f.OpenSink
	cfg.SVISource = f.OpenSource
	return nil
}

// Resourcizers lists the types in registration order.
func (f *Framework) Resourcizers(context.Context) modulert.ResourcizerList {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]modulert.ResourcizerType, 0, len(f.order))
	for _, id := range f.order {
		types = append(types, f.types[id].spec())
	}
	encoded, _ := json.Marshal(types)
	sum := sha256.Sum256(encoded)
	return modulert.ResourcizerList{ETag: hex.EncodeToString(sum[:8]), Types: types}
}

// canonicalConfig is the config's comparison form: same meaning, same bytes.
func canonicalConfig(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "{}"
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func configValues(raw json.RawMessage) map[string]string {
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	values := map[string]string{}
	for key, value := range object {
		values[key] = fmt.Sprint(value)
	}
	return values
}

// admit decides whether a spec can become an instance at all: a known type,
// a config the schema and the type accept. Its error is the failed reason.
func (f *Framework) admit(ctx context.Context, spec modulert.InstanceSpec, e entry) (any, FieldErrors, string) {
	if !namePattern.MatchString(spec.Name) {
		return nil, nil, "invalid_name"
	}
	if errs := coresvi.ValidateConfig(roundTrip(e.spec().ConfigSchema), spec.Config); len(errs) > 0 {
		fields := make(FieldErrors, 0, len(errs))
		for _, err := range errs {
			fields = append(fields, modulert.FieldError{Path: err.Path, Message: err.Message})
		}
		return nil, fields, "config_invalid"
	}
	config, err := e.decode(spec.Config)
	if err != nil {
		return nil, Field("", err.Error()), "config_invalid"
	}
	if err := e.validate(ctx, spec.Name, config); err != nil {
		var fields FieldErrors
		if errors.As(err, &fields) {
			return nil, fields, "config_invalid"
		}
		return nil, Field("", reasonOf(err)), "config_invalid"
	}
	return config, nil, ""
}

// ValidateInstance checks one spec without creating anything.
func (f *Framework) ValidateInstance(ctx context.Context, spec modulert.InstanceSpec) (int, modulert.InstanceValidateResponse) {
	f.mu.Lock()
	e, ok := f.types[spec.Type]
	f.mu.Unlock()
	if !ok {
		return http.StatusNotFound, modulert.InstanceValidateResponse{Detail: "type_missing"}
	}
	_, fields, reason := f.admit(ctx, spec, e)
	if reason != "" {
		if len(fields) == 0 {
			fields = Field("", reason)
		}
		return http.StatusUnprocessableEntity, modulert.InstanceValidateResponse{Errors: fields, Detail: reason}
	}
	return http.StatusOK, modulert.InstanceValidateResponse{}
}

// ApplyInstances makes the instances the given set.
func (f *Framework) ApplyInstances(ctx context.Context, set modulert.InstanceSet) modulert.InstanceSetResult {
	type admitted struct {
		spec   modulert.InstanceSpec
		entry  entry
		config any
		reason string
	}
	// Admission runs outside the lock: Validate is module code and may be slow.
	f.mu.Lock()
	entries := make(map[string]entry, len(f.types))
	for id, e := range f.types {
		entries[id] = e
	}
	f.mu.Unlock()
	desired := make([]admitted, 0, len(set.Instances))
	names := map[string]bool{}
	perType := map[string]int{}
	for _, spec := range set.Instances {
		item := admitted{spec: spec}
		e, ok := entries[spec.Type]
		switch {
		case names[spec.Name]:
			item.reason = "duplicate_name"
		case !ok:
			item.reason = "type_missing"
		default:
			item.entry = e
			perType[spec.Type]++
			if limit := e.maxInstances(); limit > 0 && perType[spec.Type] > limit {
				item.reason = "max_instances"
			} else {
				item.config, _, item.reason = f.admit(ctx, spec, e)
			}
		}
		names[spec.Name] = true
		desired = append(desired, item)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	next := make(map[string]*instance, len(desired))
	var started []*instance
	statuses := make([]modulert.InstanceStatus, 0, len(desired))
	for _, item := range desired {
		canonical := canonicalConfig(item.spec.Config)
		current := f.instances[item.spec.Name]
		if current != nil && next[item.spec.Name] == nil && current.spec.Type == item.spec.Type && current.canonical == canonical &&
			(item.reason == "") == (current.config != nil) && (item.reason == "" || item.reason == current.reason) {
			// Unchanged: keep the instance, its streams and its clock.
			next[item.spec.Name] = current
			statuses = append(statuses, current.status())
			continue
		}
		fresh := &instance{spec: item.spec, canonical: canonical, entry: item.entry, config: item.config,
			values: configValues(item.spec.Config), updatedAt: now, streams: map[ender]struct{}{}}
		switch {
		case item.reason != "":
			fresh.state, fresh.reason = modulert.InstanceFailed, item.reason
		default:
			fresh.state = modulert.InstanceStarting
			started = append(started, fresh)
		}
		if next[item.spec.Name] == nil {
			next[item.spec.Name] = fresh
		}
		statuses = append(statuses, fresh.status())
	}
	for name, current := range f.instances {
		if replacement, kept := next[name]; kept && replacement == current {
			continue
		}
		reason := ReasonWithdrawn
		if _, stays := next[name]; stays {
			reason = ReasonReplaced
		}
		current.end(reason)
	}
	f.instances = next
	for _, fresh := range started {
		f.start(fresh)
	}
	// Status of an instance started just now is "starting" in this answer;
	// a Realize that finishes at once shows on the next poll.
	return modulert.InstanceSetResult{Instances: statuses}
}

func (i *instance) status() modulert.InstanceStatus {
	status := modulert.InstanceStatus{Name: i.spec.Name, Type: i.spec.Type, State: i.state, Reason: i.reason, UpdatedAt: i.updatedAt}
	if i.config != nil {
		status.Resources = []string{i.spec.Type + "." + i.spec.Name}
	}
	return status
}

// end closes every open stream of the instance and releases it. Called with
// the framework lock held.
func (i *instance) end(reason string) {
	if i.cancel != nil {
		i.cancel()
	}
	for stream := range i.streams {
		stream.forceClose(reason)
	}
	i.streams = map[ender]struct{}{}
	if i.config != nil && i.entry != nil {
		go i.entry.release(i.spec.Name, i.config)
	}
}

// start realizes an instance off the request path. Called with the lock held.
func (f *Framework) start(i *instance) {
	ctx, cancel := context.WithCancel(context.Background())
	i.cancel = cancel
	go func() {
		err := i.entry.realize(ctx, i.spec.Name, i.config)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.instances[i.spec.Name] != i || ctx.Err() != nil {
			return // replaced or withdrawn while realizing
		}
		i.updatedAt = f.now()
		if err != nil {
			i.state, i.reason = modulert.InstanceFailed, reasonOf(err)
			return
		}
		i.state, i.reason = modulert.InstanceReady, ""
	}()
}

// Instances reports every instance's status, in name order.
func (f *Framework) Instances() []modulert.InstanceStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.instances))
	for name := range f.instances {
		names = append(names, name)
	}
	sort.Strings(names)
	statuses := make([]modulert.InstanceStatus, 0, len(names))
	for _, name := range names {
		statuses = append(statuses, f.instances[name].status())
	}
	return statuses
}

func (i *instance) endpointsNow() []Endpoint {
	var out []Endpoint
	for _, endpoint := range i.entry.endpoints() {
		matches := true
		for field, value := range endpoint.When {
			if i.values[field] != value {
				matches = false
				break
			}
		}
		if matches {
			out = append(out, endpoint)
		}
	}
	return out
}

// Resources is the descriptor poll: one resource per admitted instance, its
// status read from the same state the open decision reads.
func (f *Framework) Resources(context.Context) ([]coresvi.ResourceDescriptor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.instances))
	for name, i := range f.instances {
		if i.config != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	expires := f.now().Add(descriptorTTL)
	resources := make([]coresvi.ResourceDescriptor, 0, len(names))
	for _, name := range names {
		i := f.instances[name]
		status := coresvi.ResourceUnavailable
		if i.state == modulert.InstanceReady {
			status = coresvi.ResourceAvailable
		}
		labels := map[string]string{"resourcizer.type": i.spec.Type, "resourcizer.instance": name}
		reason := i.reason
		if reason == "" && i.state == modulert.InstanceStarting {
			reason = "instance_starting"
		}
		var endpoints []coresvi.EndpointDescriptor
		for _, endpoint := range i.endpointsNow() {
			descriptor := coresvi.EndpointDescriptor{
				EndpointID: endpoint.ID, Direction: coresvi.Direction(endpoint.Direction), Interaction: coresvi.InteractionStream,
				Encodings: endpoint.Encodings, QoSProfiles: endpoint.QoSProfiles, Sensitivity: endpoint.Sensitivity,
				MaxConsumers: endpoint.MaxConsumers, Exclusive: endpoint.Exclusive, Status: status,
			}
			for _, operation := range operationsOf(endpoint) {
				descriptor.Operations = append(descriptor.Operations, coresvi.Operation(operation))
			}
			if endpoint.Direction == Sink {
				descriptor.InputSchema = endpoint.Schema
			} else {
				descriptor.OutputSchema = endpoint.Schema
			}
			endpoints = append(endpoints, descriptor)
		}
		if len(endpoints) == 0 {
			continue // a config that selects no endpoint publishes nothing
		}
		resources = append(resources, coresvi.ResourceDescriptor{
			ResourceID: i.spec.Type + "." + name, NodeID: "local", Kind: i.entry.kind(),
			CanonicalName: "resourcizer/" + i.spec.Type + "/" + name, DisplayName: name,
			ProviderID: "module." + f.moduleID, Owner: coresvi.SubjectRef{Type: coresvi.SubjectNode, ID: "local"},
			Labels: labels, Status: status, StatusReason: reason, ExpiresAt: expires, Endpoints: endpoints,
		})
	}
	return resources, nil
}

// lookup finds the instance a canonical resource id (svi.<node>.<type>.<name>)
// names. The longest suffix wins, so type "rtsp" cannot claim
// "camera.rtsp"'s instance of the same name. Called with the lock held.
func (f *Framework) lookup(resourceID string) *instance {
	var best *instance
	for _, i := range f.instances {
		suffix := "." + i.spec.Type + "." + i.spec.Name
		if i.config != nil && strings.HasPrefix(resourceID, "svi.") && strings.HasSuffix(resourceID, suffix) {
			if best == nil || len(i.spec.Type) > len(best.spec.Type) {
				best = i
			}
		}
	}
	return best
}

// OpenSink is the sink seam: 404 (ErrSVISinkNotServed) for an endpoint this
// module does not have, 503 with the reason while the instance is not ready.
func (f *Framework) OpenSink(ctx context.Context, resourceID, endpointID string) (modulesdk.SVISink, error) {
	f.mu.Lock()
	i := f.lookup(resourceID)
	if i == nil {
		f.mu.Unlock()
		return nil, modulesdk.ErrSVISinkNotServed
	}
	served := false
	for _, endpoint := range i.endpointsNow() {
		if endpoint.ID == endpointID && endpoint.Direction == Sink {
			served = true
		}
	}
	if !served {
		f.mu.Unlock()
		return nil, modulesdk.ErrSVISinkNotServed
	}
	if i.state != modulert.InstanceReady {
		reason := i.reason
		if reason == "" {
			reason = "instance_starting"
		}
		f.mu.Unlock()
		return nil, errors.New(reason)
	}
	name, config, e := i.spec.Name, i.config, i.entry
	f.mu.Unlock()

	sink, err := e.openSink(ctx, name, config, endpointID)
	if err != nil {
		if errors.Is(err, modulesdk.ErrSVISinkNotServed) {
			return nil, err
		}
		return nil, errors.New(reasonOf(err))
	}
	tracked := &trackedSink{inner: sink}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.instances[name] != i {
		// Withdrawn or replaced while the module opened it.
		_ = sink.Close(ReasonWithdrawn)
		return nil, errors.New(ReasonWithdrawn)
	}
	tracked.owner = i
	i.streams[tracked] = struct{}{}
	tracked.framework = f
	return tracked, nil
}

// ender is an open stream the framework can end when its instance goes away.
type ender interface{ forceClose(reason string) }

// OpenSource is the source seam, judged like the sink: 404
// (ErrSVISourceNotServed) for an endpoint this module does not have, 503 with
// the reason while the instance is not ready.
func (f *Framework) OpenSource(ctx context.Context, resourceID, endpointID string) (modulesdk.SVISourceOpened, error) {
	f.mu.Lock()
	i := f.lookup(resourceID)
	if i == nil {
		f.mu.Unlock()
		return modulesdk.SVISourceOpened{}, modulesdk.ErrSVISourceNotServed
	}
	var served *Endpoint
	for _, endpoint := range i.endpointsNow() {
		if endpoint.ID == endpointID && endpoint.Direction == Source {
			copied := endpoint
			served = &copied
		}
	}
	if served == nil {
		f.mu.Unlock()
		return modulesdk.SVISourceOpened{}, modulesdk.ErrSVISourceNotServed
	}
	if i.state != modulert.InstanceReady {
		reason := i.reason
		if reason == "" {
			reason = "instance_starting"
		}
		f.mu.Unlock()
		return modulesdk.SVISourceOpened{}, errors.New(reason)
	}
	name, config, e := i.spec.Name, i.config, i.entry
	f.mu.Unlock()

	source, err := e.openSource(ctx, name, config, endpointID)
	if err != nil {
		if errors.Is(err, modulesdk.ErrSVISourceNotServed) {
			return modulesdk.SVISourceOpened{}, err
		}
		return modulesdk.SVISourceOpened{}, errors.New(reasonOf(err))
	}
	tracked := &trackedSource{inner: source}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.instances[name] != i {
		_ = source.Close(ReasonWithdrawn)
		return modulesdk.SVISourceOpened{}, errors.New(ReasonWithdrawn)
	}
	tracked.owner, tracked.framework = i, f
	i.streams[tracked] = struct{}{}
	opened := modulesdk.SVISourceOpened{Source: tracked, SchemaRef: served.Schema}
	if len(served.Encodings) > 0 {
		opened.Encoding = served.Encodings[0]
	}
	return opened, nil
}

// trackedSource is a source the framework can end. After the instance goes
// away its Next answers the reason (resource_withdrawn), never end of stream:
// a withdrawn resource did not complete.
type trackedSource struct {
	inner     StreamSource
	framework *Framework
	owner     *instance

	mu     sync.Mutex
	closed string
}

func (s *trackedSource) Next(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed != "" {
		return nil, errors.New(closed)
	}
	frame, err := s.inner.Next(ctx)
	s.mu.Lock()
	closed = s.closed
	s.mu.Unlock()
	if closed != "" {
		return nil, errors.New(closed)
	}
	return frame, err
}

// Close is the daemon's close: once, and success after.
func (s *trackedSource) Close() error {
	err := s.closeWith("closed")
	if s.framework != nil {
		s.framework.mu.Lock()
		delete(s.owner.streams, s)
		s.framework.mu.Unlock()
	}
	return err
}

func (s *trackedSource) forceClose(reason string) { _ = s.closeWith(reason) }

func (s *trackedSource) closeWith(reason string) error {
	s.mu.Lock()
	if s.closed != "" {
		s.mu.Unlock()
		return nil
	}
	s.closed = reason
	s.mu.Unlock()
	// Outside the lock: Close unblocks a Next in flight, which then reads
	// closed under the lock.
	return s.inner.Close(reason)
}

// trackedSink is a sink the framework can end. Its inner Close runs once,
// whether the daemon closes it or the instance goes away first.
type trackedSink struct {
	inner     StreamSink
	framework *Framework
	owner     *instance

	mu     sync.Mutex
	closed string
}

func (s *trackedSink) Write(sequence uint64, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed != "" {
		return errors.New(s.closed)
	}
	return s.inner.Write(context.Background(), sequence, data)
}

// Close is the daemon's close: once, and success after.
func (s *trackedSink) Close() error {
	err := s.closeWith("closed")
	if s.framework != nil {
		s.framework.mu.Lock()
		delete(s.owner.streams, s)
		s.framework.mu.Unlock()
	}
	return err
}

// forceClose is the framework's close when the instance ends. Called with the
// framework lock held; it does not touch owner.streams.
func (s *trackedSink) forceClose(reason string) { _ = s.closeWith(reason) }

func (s *trackedSink) closeWith(reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed != "" {
		return nil
	}
	s.closed = reason
	return s.inner.Close(reason)
}
