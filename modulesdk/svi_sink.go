package modulesdk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
)

// ErrSVISinkNotServed is how a module says "that endpoint is not mine". It is
// not a failure: the daemon asks every registered factory in turn, and a
// module that owns none of a resource must answer so without the binding
// recording an error against it.
var ErrSVISinkNotServed = errors.New("modulesdk: this module serves no sink for that endpoint")

// SVISink is one open destination for a binding's frames — the module-side
// twin of the daemon's StreamSink.
//
// Frames arrive in order, without gaps and without repeats: the binding
// executor checks the sequence and fails the binding before a bad frame
// reaches any sink. So a sink applies what it is given and does not reorder,
// buffer for reordering, or deduplicate.
//
// Close is called exactly once for every sink that was opened, including one
// whose Write returned an error, so a projection never has to time itself out.
type SVISink interface {
	Write(sequence uint64, data []byte) error
	Close() error
}

// SVISinkFunc opens a sink for one resource endpoint, or answers
// ErrSVISinkNotServed. resourceID is the node-canonical id (svi.<node>.<…>),
// which is what the binding was planned against — a module matching on its own
// module-local suffix must expect the namespace in front of it.
type SVISinkFunc func(ctx context.Context, resourceID, endpointID string) (SVISink, error)

// maxSVISinkFrame bounds one frame's JSON body. Input events are tens of
// bytes; the limit is here so a malformed or hostile body cannot be read into
// memory unbounded, not because anything legitimate approaches it.
const maxSVISinkFrame = 1 << 20

// sinkRegistry holds the sinks this module has open. The daemon addresses them
// by an id the module issues, so a stale id from a previous binding cannot
// name a live sink.
type sinkRegistry struct {
	mu    sync.Mutex
	open  map[string]SVISink
	locks map[string]*sync.Mutex
}

func newSinkRegistry() *sinkRegistry {
	return &sinkRegistry{open: map[string]SVISink{}, locks: map[string]*sync.Mutex{}}
}

func (r *sinkRegistry) add(sink SVISink) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.open[id] = sink
	r.locks[id] = &sync.Mutex{}
	return id, nil
}

// get returns the sink and the lock that serializes writes to it. The executor
// checks a frame's sequence under its own lock and then writes outside it, so
// two frames can be in flight at once; ordering is only preserved if the sink
// side takes them one at a time.
func (r *sinkRegistry) get(id string) (SVISink, *sync.Mutex, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sink, exists := r.open[id]
	if !exists {
		return nil, nil, false
	}
	return sink, r.locks[id], true
}

func (r *sinkRegistry) remove(id string) (SVISink, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sink, exists := r.open[id]
	if exists {
		delete(r.open, id)
		delete(r.locks, id)
	}
	return sink, exists
}

func sviSinkHandler(cfg Config, registry *sinkRegistry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request modulert.SVISinkRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, maxSVISinkFrame)).Decode(&request); err != nil {
			http.Error(w, "invalid sink request", http.StatusBadRequest)
			return
		}
		switch request.Op {
		case modulert.SVISinkOpOpen:
			sink, err := cfg.SVISink(r.Context(), request.ResourceID, request.EndpointID)
			if errors.Is(err, ErrSVISinkNotServed) || (err == nil && sink == nil) {
				// Not an error anywhere: 404 is the `false` of
				// OpenSink(resourceID, endpointID) (StreamSink, bool).
				writeJSON404(w, "no sink for "+request.ResourceID+"/"+request.EndpointID)
				return
			}
			if err != nil {
				// A module that COULD serve this endpoint and cannot right now
				// says why here, and the reason rides the binding's failure
				// reason rather than being lost to a status code. The commonest
				// case is exactly the one §8.9 warned about: the module is
				// installed and the injection plane is unavailable.
				writeJSONStatus(w, http.StatusServiceUnavailable, modulert.SVISinkResponse{Detail: err.Error()})
				return
			}
			id, err := registry.add(sink)
			if err != nil {
				_ = sink.Close()
				writeJSONStatus(w, http.StatusInternalServerError, modulert.SVISinkResponse{Detail: err.Error()})
				return
			}
			writeJSON(w, modulert.SVISinkResponse{SinkID: id})
		case modulert.SVISinkOpWrite:
			sink, lock, exists := registry.get(request.SinkID)
			if !exists {
				writeJSON404(w, "unknown sink "+request.SinkID)
				return
			}
			lock.Lock()
			err := sink.Write(request.Sequence, request.Data)
			lock.Unlock()
			if err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, modulert.SVISinkResponse{Detail: err.Error()})
				return
			}
			writeJSON(w, modulert.SVISinkResponse{})
		case modulert.SVISinkOpClose:
			sink, exists := registry.remove(request.SinkID)
			if !exists {
				// Closing an unknown sink is success. A close that races a
				// module restart must not leave the daemon retrying a teardown.
				writeJSON(w, modulert.SVISinkResponse{})
				return
			}
			if err := sink.Close(); err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, modulert.SVISinkResponse{Detail: err.Error()})
				return
			}
			writeJSON(w, modulert.SVISinkResponse{})
		default:
			http.Error(w, "unknown sink op", http.StatusBadRequest)
		}
	})
}

func writeJSON404(w http.ResponseWriter, detail string) {
	writeJSONStatus(w, http.StatusNotFound, modulert.SVISinkResponse{Detail: detail})
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
