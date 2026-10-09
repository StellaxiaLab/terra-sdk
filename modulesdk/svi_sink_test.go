package modulesdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
)

type recordingSink struct {
	mu      sync.Mutex
	frames  [][]byte
	closed  int
	failOn  uint64
	failing bool
}

func (s *recordingSink) Write(sequence uint64, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failing && sequence == s.failOn {
		return errors.New("refused")
	}
	s.frames = append(s.frames, append([]byte(nil), data...))
	return nil
}

func (s *recordingSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return nil
}

func sinkServer(t *testing.T, open SVISinkFunc) (*httptest.Server, string) {
	t.Helper()
	const credential = "test-credential"
	server := httptest.NewServer(routes(Config{
		Identity: modulert.WorkloadIdentity{Endpoint: "127.0.0.1:0", Credential: credential},
		SVISink:  open,
	}))
	t.Cleanup(server.Close)
	return server, credential
}

func sinkCall(t *testing.T, server *httptest.Server, credential string, request modulert.SVISinkRequest) (int, modulert.SVISinkResponse) {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, err := http.NewRequest(http.MethodPost, server.URL+modulert.SVISinkPath, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	httpRequest.Header.Set(modulert.CredentialHeader, credential)
	response, err := server.Client().Do(httpRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var decoded modulert.SVISinkResponse
	_ = json.NewDecoder(response.Body).Decode(&decoded)
	return response.StatusCode, decoded
}

func TestSinkSeamOpensWritesAndCloses(t *testing.T) {
	sink := &recordingSink{}
	server, credential := sinkServer(t, func(context.Context, string, string) (SVISink, error) { return sink, nil })

	status, opened := sinkCall(t, server, credential, modulert.SVISinkRequest{
		Op: modulert.SVISinkOpOpen, ResourceID: "svi.node-a.thing", EndpointID: "input"})
	if status != http.StatusOK || opened.SinkID == "" {
		t.Fatalf("open = %d %+v", status, opened)
	}
	if status, _ := sinkCall(t, server, credential, modulert.SVISinkRequest{
		Op: modulert.SVISinkOpWrite, SinkID: opened.SinkID, Sequence: 1, Data: []byte("frame-1")}); status != http.StatusOK {
		t.Fatalf("write = %d", status)
	}
	if status, _ := sinkCall(t, server, credential, modulert.SVISinkRequest{
		Op: modulert.SVISinkOpClose, SinkID: opened.SinkID}); status != http.StatusOK {
		t.Fatalf("close = %d", status)
	}
	if len(sink.frames) != 1 || string(sink.frames[0]) != "frame-1" {
		t.Fatalf("frames = %q", sink.frames)
	}
	if sink.closed != 1 {
		t.Fatalf("closed %d times, want exactly one", sink.closed)
	}
	// Writing to a closed sink must not reach it. Sink ids are issued per
	// open, so a stale one from a torn-down binding cannot name a live sink.
	if status, _ := sinkCall(t, server, credential, modulert.SVISinkRequest{
		Op: modulert.SVISinkOpWrite, SinkID: opened.SinkID, Sequence: 2, Data: []byte("late")}); status != http.StatusNotFound {
		t.Fatalf("write after close = %d, want 404", status)
	}
	if len(sink.frames) != 1 {
		t.Fatalf("a frame reached a closed sink: %q", sink.frames)
	}
}

// "Not mine" and "mine but broken" are different answers with different
// consequences: the daemon keeps looking for a backend on the first and fails
// the binding with a reason on the second.
func TestSinkSeamSeparatesNotMineFromUnavailable(t *testing.T) {
	server, credential := sinkServer(t, func(_ context.Context, resourceID, _ string) (SVISink, error) {
		if resourceID == "svi.node-a.mine" {
			return nil, errors.New("input injection unavailable: /dev/uinput is not writable")
		}
		return nil, ErrSVISinkNotServed
	})

	status, body := sinkCall(t, server, credential, modulert.SVISinkRequest{
		Op: modulert.SVISinkOpOpen, ResourceID: "svi.node-a.somebody-elses", EndpointID: "input"})
	if status != http.StatusNotFound {
		t.Fatalf("declined open = %d, want 404 (the bool of OpenSink)", status)
	}

	status, body = sinkCall(t, server, credential, modulert.SVISinkRequest{
		Op: modulert.SVISinkOpOpen, ResourceID: "svi.node-a.mine", EndpointID: "input"})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("unavailable open = %d, want 503", status)
	}
	if body.Detail == "" {
		t.Fatal("an unavailable sink answered without saying why; the reason is the whole point")
	}
}

// A close that arrives twice, or after the module restarted, is success. Any
// other answer has the daemon retrying a teardown forever.
func TestClosingAnUnknownSinkSucceeds(t *testing.T) {
	server, credential := sinkServer(t, func(context.Context, string, string) (SVISink, error) {
		return &recordingSink{}, nil
	})
	if status, _ := sinkCall(t, server, credential, modulert.SVISinkRequest{
		Op: modulert.SVISinkOpClose, SinkID: "never-existed"}); status != http.StatusOK {
		t.Fatalf("close of unknown sink = %d, want 200", status)
	}
}

func TestSinkSeamRequiresTheWorkloadCredential(t *testing.T) {
	server, _ := sinkServer(t, func(context.Context, string, string) (SVISink, error) {
		t.Fatal("an unauthenticated request reached the module's sink")
		return nil, nil
	})
	if status, _ := sinkCall(t, server, "wrong", modulert.SVISinkRequest{
		Op: modulert.SVISinkOpOpen, ResourceID: "svi.node-a.thing", EndpointID: "input"}); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated open = %d, want 401", status)
	}
}

// A module that serves no sink must not answer the path at all: mounting it
// unconditionally would make every module look like a possible destination.
func TestSinkPathIsAbsentWithoutASinkFunc(t *testing.T) {
	server := httptest.NewServer(routes(Config{
		Identity: modulert.WorkloadIdentity{Endpoint: "127.0.0.1:0", Credential: "c"},
	}))
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+modulert.SVISinkPath, bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(modulert.CredentialHeader, "c")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("sink path answered %d on a module with no sink", response.StatusCode)
	}
}
