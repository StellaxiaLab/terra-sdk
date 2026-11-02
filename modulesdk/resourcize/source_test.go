package resourcize_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/StellaxiaLab/terra-sdk/conformance"
	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
	"github.com/StellaxiaLab/terra-sdk/modulesdk"
	"github.com/StellaxiaLab/terra-sdk/modulesdk/resourcize"
)

type counterConfig struct {
	Limit int `json:"limit,omitempty"`
}

// counterSource emits "1", "2", ... and ends after Limit frames (0 = never).
type counterSource struct {
	limit  int
	mu     sync.Mutex
	n      int
	closed chan struct{}
	once   sync.Once
}

func (s *counterSource) Next(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	if s.limit > 0 && s.n >= s.limit {
		s.mu.Unlock()
		return nil, io.EOF
	}
	s.n++
	n := s.n
	s.mu.Unlock()
	select {
	case <-time.After(2 * time.Millisecond):
	case <-s.closed:
		return nil, errors.New("closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []byte(strconv.Itoa(n)), nil
}

func (s *counterSource) Close(string) error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func registerCounter(t *testing.T, framework *resourcize.Framework) {
	t.Helper()
	err := resourcize.Register(framework, resourcize.Declared[counterConfig]{
		ID: "sample.counter", Title: "Counter",
		Schema:    map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer", "minimum": 0}}},
		Endpoints: []resourcize.Endpoint{{ID: "out", Direction: resourcize.Source, Schema: "terra.bytes@1", Encodings: []string{"text/plain"}}},
		OpenSource: func(_ context.Context, instance resourcize.Instance[counterConfig], _ string) (resourcize.StreamSource, error) {
			return &counterSource{limit: instance.Config.Limit, closed: make(chan struct{})}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func serveFramework(t *testing.T, framework *resourcize.Framework) conformance.Identity {
	t.Helper()
	id, err := conformance.NewIdentity("lab.example.counter", 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg := modulesdk.Config{Identity: modulert.WorkloadIdentity{
		ModuleID: id.ModuleID, InstanceID: id.InstanceID, Epoch: id.Epoch, Nonce: id.Nonce, Credential: id.Credential, Endpoint: id.Endpoint,
	}}
	if err := framework.Install(&cfg); err != nil {
		t.Fatal(err)
	}
	server, err := modulesdk.Listen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Serve(ctx) }()
	return id
}

// A framework module with a source type passes the source rules of the kit.
func TestSourceTypePassesTheConformance(t *testing.T) {
	framework := resourcize.New("lab.example.counter")
	registerCounter(t, framework)
	id := serveFramework(t, framework)
	report := conformance.Check(context.Background(), id, conformance.CheckOptions{SVI: true, Resourcizer: &conformance.ResourcizerPlan{
		Samples: []conformance.InstanceSample{{Name: "c", Type: "sample.counter", Config: json.RawMessage(`{}`)}},
		Invalid: []conformance.InstanceSample{{Name: "bad", Type: "sample.counter", Config: json.RawMessage(`{"limit":-1}`)}},
	}})
	if !report.OK() {
		t.Fatalf("conformance:\n%s", report.String())
	}
	for _, result := range report.Results {
		if result.Level() == conformance.StatusSkip && strings.HasPrefix(result.Name, "source") {
			t.Fatalf("the source rules were skipped:\n%s", report.String())
		}
	}
}

func openCounter(t *testing.T, framework *resourcize.Framework, name string) modulesdk.SVISource {
	t.Helper()
	opened, err := framework.OpenSource(context.Background(), "svi.n.sample.counter."+name, "out")
	if err != nil {
		t.Fatal(err)
	}
	if opened.SchemaRef != "terra.bytes@1" || opened.Encoding != "text/plain" {
		t.Fatalf("opened = %+v", opened)
	}
	return opened.Source
}

// End of stream is io.EOF; a withdrawn instance's source answers the reason
// instead — never io.EOF, which would complete the binding as if done.
func TestSourceEndsWithEOFOrWithTheReason(t *testing.T) {
	framework := resourcize.New("m")
	registerCounter(t, framework)
	ctx := context.Background()
	framework.ApplyInstances(ctx, modulert.InstanceSet{Instances: []modulert.InstanceSpec{
		{Name: "short", Type: "sample.counter", Config: json.RawMessage(`{"limit":3}`)},
		{Name: "long", Type: "sample.counter", Config: json.RawMessage(`{}`)},
	}})
	waitState(t, framework, "short", modulert.InstanceReady)
	waitState(t, framework, "long", modulert.InstanceReady)

	short := openCounter(t, framework, "short")
	var got []string
	for {
		frame, err := short.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(frame))
	}
	if strings.Join(got, ",") != "1,2,3" {
		t.Fatalf("frames = %v", got)
	}

	long := openCounter(t, framework, "long")
	if _, err := long.Next(ctx); err != nil {
		t.Fatal(err)
	}
	framework.ApplyInstances(ctx, modulert.InstanceSet{Instances: []modulert.InstanceSpec{{Name: "short", Type: "sample.counter", Config: json.RawMessage(`{"limit":3}`)}}})
	_, err := long.Next(ctx)
	if err == nil || errors.Is(err, io.EOF) || err.Error() != resourcize.ReasonWithdrawn {
		t.Fatalf("withdrawn source = %v, want %s", err, resourcize.ReasonWithdrawn)
	}
	if err := long.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := framework.OpenSource(ctx, "svi.n.sample.counter.long", "out"); !errors.Is(err, modulesdk.ErrSVISourceNotServed) {
		t.Fatalf("open of a withdrawn instance = %v", err)
	}
	if _, err := framework.OpenSource(ctx, "svi.n.sample.counter.short", "nope"); !errors.Is(err, modulesdk.ErrSVISourceNotServed) {
		t.Fatalf("open of a missing endpoint = %v", err)
	}
}

func TestRegisterRefusesASourceWithoutOpenSource(t *testing.T) {
	err := resourcize.Register(resourcize.New("m"), resourcize.Declared[counterConfig]{
		ID: "a.b", Title: "x", Schema: map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer"}}},
		Endpoints: []resourcize.Endpoint{{ID: "out", Direction: resourcize.Source, Schema: "terra.bytes@1"}},
	})
	if err == nil {
		t.Fatal("a source endpoint without OpenSource was registered")
	}
}
