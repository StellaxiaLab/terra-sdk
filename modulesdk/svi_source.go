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
	"time"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
)

// ErrSVISourceNotServed is how a module says "that endpoint is not mine".
var ErrSVISourceNotServed = errors.New("modulesdk: this module serves no source for that endpoint")

// SVISource is one open origin of a binding's frames. Next returns the next
// frame, io.EOF when the stream ended on its own, or another error when it
// failed (the text becomes the binding's reason). Close is called exactly
// once for every source that was opened.
type SVISource interface {
	Next(ctx context.Context) ([]byte, error)
	Close() error
}

// SVISourceOpened is what SVISourceFunc answers: the source and how its
// frames are described.
type SVISourceOpened struct {
	Source    SVISource
	SchemaRef string
	Encoding  string
}

// SVISourceFunc opens a source for one resource endpoint, or answers
// ErrSVISourceNotServed. resourceID is the node-canonical id.
type SVISourceFunc func(ctx context.Context, resourceID, endpointID string) (SVISourceOpened, error)

const (
	defaultSourceBatch = 64
	maxSourceBatch     = 1024
	defaultSourceWait  = time.Second
	maxSourceWait      = 10 * time.Second
	// sourceBuffer is how far a producer may run ahead of the polls.
	sourceBuffer = 256
)

// openSource pumps one source into a buffer, so a next that times out never
// abandons a frame halfway through being read.
type openSource struct {
	frames chan []byte
	done   chan struct{} // closed when the producer stops
	err    error         // io.EOF or the failure, set before done closes
	stop   context.CancelFunc
	once   sync.Once
	source SVISource
}

func startSource(source SVISource) *openSource {
	ctx, cancel := context.WithCancel(context.Background())
	s := &openSource{frames: make(chan []byte, sourceBuffer), done: make(chan struct{}), stop: cancel, source: source}
	go func() {
		defer close(s.done)
		for {
			frame, err := source.Next(ctx)
			if err != nil {
				s.err = err
				return
			}
			select {
			case s.frames <- frame:
			case <-ctx.Done():
				s.err = ctx.Err()
				return
			}
		}
	}()
	return s
}

func (s *openSource) close() error {
	var err error
	s.once.Do(func() {
		s.stop()
		err = s.source.Close()
	})
	return err
}

// next answers up to max frames, waiting up to wait for the first one, and
// whether the source ended (io.EOF) — or the failure.
func (s *openSource) next(max int, wait time.Duration) ([][]byte, bool, error) {
	var out [][]byte
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for len(out) < max {
		if len(out) > 0 {
			// Hand over what is ready; do not hold a batch to fill it.
			select {
			case frame := <-s.frames:
				out = append(out, frame)
				continue
			case <-s.done:
				return s.drain(out, max)
			default:
				return out, false, nil
			}
		}
		select {
		case frame := <-s.frames:
			out = append(out, frame)
		case <-s.done:
			return s.drain(out, max)
		case <-timer.C:
			return out, false, nil
		}
	}
	return out, false, nil
}

// drain takes what the producer left before it stopped, then says why.
func (s *openSource) drain(out [][]byte, max int) ([][]byte, bool, error) {
	for len(out) < max {
		select {
		case frame := <-s.frames:
			out = append(out, frame)
			continue
		default:
		}
		break
	}
	if len(s.frames) > 0 {
		return out, false, nil // more than fits: the next call gets it
	}
	if errors.Is(s.err, io.EOF) {
		return out, true, nil
	}
	return out, false, s.err
}

type sourceRegistry struct {
	mu   sync.Mutex
	open map[string]*openSource
}

func newSourceRegistry() *sourceRegistry { return &sourceRegistry{open: map[string]*openSource{}} }

func (r *sourceRegistry) add(source *openSource) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)
	r.mu.Lock()
	r.open[id] = source
	r.mu.Unlock()
	return id, nil
}

func (r *sourceRegistry) get(id string) (*openSource, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	source, ok := r.open[id]
	return source, ok
}

func (r *sourceRegistry) remove(id string) (*openSource, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	source, ok := r.open[id]
	delete(r.open, id)
	return source, ok
}

func sviSourceHandler(cfg Config, registry *sourceRegistry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request modulert.SVISourceRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&request); err != nil {
			http.Error(w, "invalid source request", http.StatusBadRequest)
			return
		}
		switch request.Op {
		case modulert.SVISourceOpOpen:
			opened, err := cfg.SVISource(r.Context(), request.ResourceID, request.EndpointID)
			if errors.Is(err, ErrSVISourceNotServed) || (err == nil && opened.Source == nil) {
				writeJSONStatus(w, http.StatusNotFound, modulert.SVISourceResponse{Detail: "no source for " + request.ResourceID + "/" + request.EndpointID})
				return
			}
			if err != nil {
				writeJSONStatus(w, http.StatusServiceUnavailable, modulert.SVISourceResponse{Detail: err.Error()})
				return
			}
			id, err := registry.add(startSource(opened.Source))
			if err != nil {
				_ = opened.Source.Close()
				writeJSONStatus(w, http.StatusInternalServerError, modulert.SVISourceResponse{Detail: err.Error()})
				return
			}
			writeJSON(w, modulert.SVISourceResponse{SourceID: id, SchemaRef: opened.SchemaRef, Encoding: opened.Encoding})
		case modulert.SVISourceOpNext:
			source, ok := registry.get(request.SourceID)
			if !ok {
				writeJSONStatus(w, http.StatusNotFound, modulert.SVISourceResponse{Detail: "unknown source " + request.SourceID})
				return
			}
			max := request.MaxFrames
			if max <= 0 {
				max = defaultSourceBatch
			}
			if max > maxSourceBatch {
				max = maxSourceBatch
			}
			wait := time.Duration(request.WaitMillis) * time.Millisecond
			if wait <= 0 {
				wait = defaultSourceWait
			}
			if wait > maxSourceWait {
				wait = maxSourceWait
			}
			frames, done, err := source.next(max, wait)
			if err != nil {
				// A failed source answers its reason, never "done": a failure
				// must not read as completion.
				writeJSONStatus(w, http.StatusInternalServerError, modulert.SVISourceResponse{Detail: err.Error()})
				return
			}
			writeJSON(w, modulert.SVISourceResponse{Frames: frames, Done: done})
		case modulert.SVISourceOpClose:
			if source, ok := registry.remove(request.SourceID); ok {
				_ = source.close()
			}
			writeJSON(w, modulert.SVISourceResponse{})
		default:
			http.Error(w, "unknown source op", http.StatusBadRequest)
		}
	})
}
