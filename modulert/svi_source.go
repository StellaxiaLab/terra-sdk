package modulert

// The host→module SVI source seam — the twin of the sink (resourcizer
// framework design §7, R4).
//
// A module could describe resources and absorb a binding's frames, but it
// could not be a binding's ORIGIN: the daemon had no way to pull frames out of
// it. This path does that. The daemon opens a source on the module, asks for
// frames with next, and closes it. next is a long poll that answers a batch,
// so a stream costs one round trip per batch, not per frame. The daemon
// numbers the frames; the module only produces them in order.
const SVISourcePath = "/terra/svi/source"

// SVISourceOp is which of the three things a source request does.
type SVISourceOp string

const (
	// SVISourceOpOpen asks whether the module produces data for one resource
	// endpoint and starts a source if it does. 404 means "not mine"; 503 with
	// a detail means "mine, not now".
	SVISourceOpOpen SVISourceOp = "open"
	// SVISourceOpNext waits up to WaitMillis for up to MaxFrames frames.
	SVISourceOpNext SVISourceOp = "next"
	// SVISourceOpClose ends a source. It is sent for every opened source, and
	// closing one twice (or an unknown one) succeeds.
	SVISourceOpClose SVISourceOp = "close"
)

// SVISourceRequest is one host→module source operation.
type SVISourceRequest struct {
	Op SVISourceOp `json:"op"`
	// ResourceID is the daemon's canonical id (svi.<node>.<suffix>).
	ResourceID string `json:"resourceId,omitempty"`
	EndpointID string `json:"endpointId,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
	// MaxFrames and WaitMillis bound one next; zero means the module's
	// defaults (64 frames, 1000 ms).
	MaxFrames  int `json:"maxFrames,omitempty"`
	WaitMillis int `json:"waitMillis,omitempty"`
}

// SVISourceResponse is the module's reply.
type SVISourceResponse struct {
	SourceID string `json:"sourceId,omitempty"`
	// SchemaRef and Encoding describe the frames; answered on open.
	SchemaRef string `json:"schemaRef,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
	// Frames are the next frames, in order, payloads base64 in JSON.
	Frames [][]byte `json:"frames,omitempty"`
	// Done says the source ended on its own (end of stream): the frames in
	// this answer are the last ones. A source that fails answers non-2xx with
	// the reason in Detail instead, so a failure never reads as completion.
	Done   bool   `json:"done,omitempty"`
	Detail string `json:"detail,omitempty"`
}
