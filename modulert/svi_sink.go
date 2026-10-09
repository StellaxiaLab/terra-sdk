package modulert

// The host→module SVI sink seam.
//
// A module can already tell the node what resources it owns (SVIResourcesPath)
// and the daemon's remote provider adapter puts them in the catalog. What it
// could not do was be the DESTINATION of a binding. OpenSink is answered by
// in-daemon backends only, so a resource a module published could be planned
// into a binding and then refused at prepare with "backend unavailable": the
// catalog said the endpoint was there and the data path said it was not. A
// resource nobody can bind to is worse than a resource nobody published,
// because the failure arrives after the grant, the plan and the lease.
//
// This is the twin of that pull. The daemon opens a sink on the module, writes
// the binding's frames to it, and closes it. Frames arrive already ordered and
// de-duplicated — the binding executor checks the sequence before it reaches
// any sink — so a module sink is an ordered writer and nothing more.
//
// Three operations on one path rather than three paths: the SDK mounts a
// single guarded handler, and a module that sinks nothing mounts none.
const SVISinkPath = "/terra/svi/sink"

// SVISinkOp is which of the three things a sink request does.
type SVISinkOp string

const (
	// SVISinkOpOpen asks whether the module absorbs data for one resource
	// endpoint, and starts a sink if it does. The answer is the (sink, bool)
	// of the daemon's StreamSinkFactory: 404 means "not mine", which is not an
	// error — another factory may own it.
	SVISinkOpOpen SVISinkOp = "open"
	// SVISinkOpWrite delivers one frame to an open sink.
	SVISinkOpWrite SVISinkOp = "write"
	// SVISinkOpClose ends a sink. It is sent for every open sink, including
	// failed ones, so a module never has to time a projection out.
	SVISinkOpClose SVISinkOp = "close"
)

// SVISinkRequest is one host→module sink operation.
type SVISinkRequest struct {
	Op SVISinkOp `json:"op"`
	// ResourceID and EndpointID identify the endpoint being opened. They carry
	// the daemon's canonical resource id (svi.<node>.<suffix>), not the
	// module-local suffix the module published: the module answers about the
	// resource the catalog knows, because that is the name the binding was
	// planned against.
	ResourceID string `json:"resourceId,omitempty"`
	EndpointID string `json:"endpointId,omitempty"`
	// SinkID addresses an open sink for write and close.
	SinkID string `json:"sinkId,omitempty"`
	// Sequence is the frame's position in the binding's stream. It is
	// contiguous from the sink's point of view; the executor fails the binding
	// on a gap rather than passing one on.
	Sequence uint64 `json:"sequence,omitempty"`
	// Data is the frame payload, base64 in JSON.
	Data []byte `json:"data,omitempty"`
}

// SVISinkResponse is the module's reply.
type SVISinkResponse struct {
	// SinkID names the sink an open created; empty on write and close.
	SinkID string `json:"sinkId,omitempty"`
	// Detail is a human-readable note, carried on refusals so the reason
	// reaches the binding's status instead of dying in a status code.
	Detail string `json:"detail,omitempty"`
}
