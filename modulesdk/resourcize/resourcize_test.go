package resourcize_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/StellaxiaLab/terra-sdk/conformance"
	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
	"github.com/StellaxiaLab/terra-sdk/modulesdk"
	"github.com/StellaxiaLab/terra-sdk/modulesdk/resourcize"
	coresvi "github.com/StellaxiaLab/terra-sdk/svi"
)

// recorderConfig is the sample type's config: a sink that records frames.
type recorderConfig struct {
	Label string `json:"label"`
	Mode  string `json:"mode,omitempty"`
	Token string `json:"token,omitempty"`
	Fail  bool   `json:"fail,omitempty"`
}

var recorderSchema = map[string]any{
	"type":     "object",
	"required": []any{"label"},
	"properties": map[string]any{
		"label": map[string]any{"type": "string", "title": "Label", "minLength": 1},
		"mode":  map[string]any{"type": "string", "enum": []any{"keep", "drop"}},
		"token": map[string]any{"type": "string", "x-terra-secret": true},
		"fail":  map[string]any{"type": "boolean"},
	},
}

type recorder struct {
	mu      sync.Mutex
	frames  map[string][]string
	closed  map[string]string
	release []string
	// realizeGate, when set, holds Realize until it is closed.
	realizeGate chan struct{}
}

func newRecorder() *recorder {
	return &recorder{frames: map[string][]string{}, closed: map[string]string{}}
}

type recordingSink struct {
	r    *recorder
	name string
}

func (s recordingSink) Write(_ context.Context, _ uint64, data []byte) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	s.r.frames[s.name] = append(s.r.frames[s.name], string(data))
	return nil
}

func (s recordingSink) Close(reason string) error {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	s.r.closed[s.name] += reason + ";"
	return nil
}

func sampleFramework(t *testing.T, r *recorder) *resourcize.Framework {
	t.Helper()
	framework := resourcize.New("lab.example.recorder")
	err := resourcize.Register(framework, resourcize.Declared[recorderConfig]{
		ID: "sample.recorder", Title: "Recorder", Schema: recorderSchema,
		Endpoints: []resourcize.Endpoint{{ID: "in", Direction: resourcize.Sink, Schema: "terra.bytes@1"}},
		Validate: func(_ context.Context, instance resourcize.Instance[recorderConfig]) error {
			if instance.Config.Label == "forbidden" {
				return resourcize.Field("/label", "this label is reserved")
			}
			return nil
		},
		Realize: func(ctx context.Context, instance resourcize.Instance[recorderConfig]) error {
			if r.realizeGate != nil {
				select {
				case <-r.realizeGate:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if instance.Config.Fail {
				return resourcize.Unavailable("probe_refused")
			}
			return nil
		},
		Release: func(instance resourcize.Instance[recorderConfig]) {
			r.mu.Lock()
			r.release = append(r.release, instance.Name)
			r.mu.Unlock()
		},
		OpenSink: func(_ context.Context, instance resourcize.Instance[recorderConfig], _ string) (resourcize.StreamSink, error) {
			return recordingSink{r: r, name: instance.Name}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return framework
}

func spec(name, config string) modulert.InstanceSpec {
	return modulert.InstanceSpec{Name: name, Type: "sample.recorder", Config: json.RawMessage(config)}
}

func waitState(t *testing.T, framework *resourcize.Framework, name, state string) modulert.InstanceStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		for _, status := range framework.Instances() {
			if status.Name == name && status.State == state {
				return status
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never reached %s: %+v", name, state, framework.Instances())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The framework passes the conformance kit it ships with: served for real
// through modulesdk, with a sink sample, an invalid sample and a secret.
func TestFrameworkModulePassesTheResourcizerConformance(t *testing.T) {
	r := newRecorder()
	framework := sampleFramework(t, r)
	id, err := conformance.NewIdentity("lab.example.recorder", 1)
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

	report := conformance.Check(context.Background(), id, conformance.CheckOptions{
		SVI: true,
		Resourcizer: &conformance.ResourcizerPlan{
			Samples: []conformance.InstanceSample{
				{Name: "front", Type: "sample.recorder", Config: json.RawMessage(`{"label":"front","token":"s3cret-token"}`)},
				{Name: "back", Type: "sample.recorder", Config: json.RawMessage(`{"label":"back","mode":"keep"}`)},
			},
			Invalid: []conformance.InstanceSample{
				{Name: "bad", Type: "sample.recorder", Config: json.RawMessage(`{"mode":"loud"}`)},
				{Name: "reserved", Type: "sample.recorder", Config: json.RawMessage(`{"label":"forbidden"}`)},
			},
		},
	})
	if !report.OK() {
		t.Fatalf("conformance:\n%s", report.String())
	}
	for _, result := range report.Results {
		if result.Level() == conformance.StatusSkip && strings.HasPrefix(result.Name, "sink") {
			t.Fatalf("the sink rules were skipped:\n%s", report.String())
		}
	}
}

func TestRegisterRefusesWhatTheNodeWould(t *testing.T) {
	sink := []resourcize.Endpoint{{ID: "in", Direction: resourcize.Sink, Schema: "terra.bytes@1"}}
	open := func(context.Context, resourcize.Instance[recorderConfig], string) (resourcize.StreamSink, error) {
		return nil, nil
	}
	cases := map[string]resourcize.Declared[recorderConfig]{
		"reserved prefix": {ID: "fs.thing", Title: "x", Schema: recorderSchema, Endpoints: sink, OpenSink: open},
		"bad id":          {ID: "Camera", Title: "x", Schema: recorderSchema, Endpoints: sink, OpenSink: open},
		"no title":        {ID: "a.b", Schema: recorderSchema, Endpoints: sink, OpenSink: open},
		"ref schema":      {ID: "a.b", Title: "x", Schema: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"$ref": "x"}}}, Endpoints: sink, OpenSink: open},
		"no endpoints":    {ID: "a.b", Title: "x", Schema: recorderSchema, OpenSink: open},
		"sink, no open":   {ID: "a.b", Title: "x", Schema: recorderSchema, Endpoints: sink},
		"bad schema ref":  {ID: "a.b", Title: "x", Schema: recorderSchema, Endpoints: []resourcize.Endpoint{{ID: "in", Direction: resourcize.Sink, Schema: "bytes"}}, OpenSink: open},
		"when unknown":    {ID: "a.b", Title: "x", Schema: recorderSchema, Endpoints: []resourcize.Endpoint{{ID: "in", Direction: resourcize.Sink, Schema: "terra.bytes@1", When: map[string]string{"nope": "x"}}}, OpenSink: open},
	}
	for name, declared := range cases {
		if err := resourcize.Register(resourcize.New("m"), declared); err == nil {
			t.Errorf("%s: registered", name)
		}
	}
	framework := resourcize.New("m")
	good := resourcize.Declared[recorderConfig]{ID: "a.b", Title: "x", Schema: recorderSchema, Endpoints: sink, OpenSink: open}
	if err := resourcize.Register(framework, good); err != nil {
		t.Fatal(err)
	}
	if err := resourcize.Register(framework, good); err == nil {
		t.Fatal("a type registered twice")
	}
}

// A changed config replaces the instance and ends its sink with
// resource_replaced; a removed one ends with resource_withdrawn; an unchanged
// one keeps its sink open and its status time.
func TestInstanceSetLifecycleClosesStreamsWithTheirReason(t *testing.T) {
	r := newRecorder()
	framework := sampleFramework(t, r)
	ctx := context.Background()
	framework.ApplyInstances(ctx, modulert.InstanceSet{Instances: []modulert.InstanceSpec{spec("a", `{"label":"a"}`), spec("b", `{"label":"b"}`)}})
	readyA := waitState(t, framework, "a", modulert.InstanceReady)
	waitState(t, framework, "b", modulert.InstanceReady)

	sinkA, err := framework.OpenSink(ctx, "svi.node-1.sample.recorder.a", "in")
	if err != nil {
		t.Fatal(err)
	}
	sinkB, err := framework.OpenSink(ctx, "svi.node-1.sample.recorder.b", "in")
	if err != nil {
		t.Fatal(err)
	}

	// Same set, key order shuffled: nothing changes.
	framework.ApplyInstances(ctx, modulert.InstanceSet{Instances: []modulert.InstanceSpec{spec("a", `{ "label" : "a" }`), spec("b", `{"label":"b"}`)}})
	if again := waitState(t, framework, "a", modulert.InstanceReady); !again.UpdatedAt.Equal(readyA.UpdatedAt) {
		t.Fatalf("an unchanged instance got a new time: %v then %v", readyA.UpdatedAt, again.UpdatedAt)
	}
	if err := sinkA.Write(1, []byte("one")); err != nil {
		t.Fatalf("the sink of an unchanged instance closed: %v", err)
	}

	// a changes, b goes away.
	framework.ApplyInstances(ctx, modulert.InstanceSet{Instances: []modulert.InstanceSpec{spec("a", `{"label":"a2"}`)}})
	r.mu.Lock()
	closedA, closedB := r.closed["a"], r.closed["b"]
	r.mu.Unlock()
	if closedA != resourcize.ReasonReplaced+";" || closedB != resourcize.ReasonWithdrawn+";" {
		t.Fatalf("closed a=%q b=%q", closedA, closedB)
	}
	if err := sinkB.Write(2, []byte("late")); err == nil || err.Error() != resourcize.ReasonWithdrawn {
		t.Fatalf("write to a withdrawn sink = %v", err)
	}
	// The daemon's own close after the framework's: once, and success.
	if err := sinkB.Close(); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if r.closed["b"] != resourcize.ReasonWithdrawn+";" {
		t.Fatalf("inner close ran twice: %q", r.closed["b"])
	}
	r.mu.Unlock()
	if _, err := framework.OpenSink(ctx, "svi.node-1.sample.recorder.b", "in"); !errors.Is(err, modulesdk.ErrSVISinkNotServed) {
		t.Fatalf("open of a withdrawn instance = %v, want not served", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		released := strings.Join(r.release, ",")
		r.mu.Unlock()
		if strings.Contains(released, "a") && strings.Contains(released, "b") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("release = %q, want both the replaced and the withdrawn instance", released)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The descriptor and the open decision read one state: a failed instance is
// an unavailable resource with its reason, and opening it answers that reason.
func TestFailedInstanceIsUnavailableEverywhere(t *testing.T) {
	r := newRecorder()
	framework := sampleFramework(t, r)
	ctx := context.Background()
	framework.ApplyInstances(ctx, modulert.InstanceSet{Instances: []modulert.InstanceSpec{spec("cam", `{"label":"x","fail":true}`)}})
	status := waitState(t, framework, "cam", modulert.InstanceFailed)
	if status.Reason != "probe_refused" {
		t.Fatalf("reason = %q", status.Reason)
	}
	resources, err := framework.Resources(ctx)
	if err != nil || len(resources) != 1 {
		t.Fatalf("resources = %v %v", resources, err)
	}
	resource := resources[0]
	if resource.Status != coresvi.ResourceUnavailable || resource.Labels["status_reason"] != "probe_refused" || resource.ResourceID != "sample.recorder.cam" {
		t.Fatalf("descriptor = %+v", resource)
	}
	if err := coresvi.ValidateResource(resource); err != nil {
		t.Fatalf("descriptor invalid: %v", err)
	}
	if _, err := framework.OpenSink(ctx, "svi.n.sample.recorder.cam", "in"); err == nil || err.Error() != "probe_refused" || errors.Is(err, modulesdk.ErrSVISinkNotServed) {
		t.Fatalf("open of a failed instance = %v, want its reason (503)", err)
	}

	// A config the schema refuses never becomes a resource at all.
	result := framework.ApplyInstances(ctx, modulert.InstanceSet{Instances: []modulert.InstanceSpec{spec("bad", `{"mode":"loud"}`), {Name: "ghost", Type: "no.such", Config: json.RawMessage(`{}`)}}})
	if result.Instances[0].Reason != "config_invalid" || result.Instances[1].Reason != "type_missing" {
		t.Fatalf("statuses = %+v", result.Instances)
	}
	if resources, _ := framework.Resources(ctx); len(resources) != 0 {
		t.Fatalf("refused configs published %v", resources)
	}
}

// A Realize that finishes after its instance was withdrawn must not bring it
// back: the state belongs to whatever instance holds the name now.
func TestLateRealizeDoesNotResurrect(t *testing.T) {
	r := newRecorder()
	r.realizeGate = make(chan struct{})
	framework := sampleFramework(t, r)
	ctx := context.Background()
	framework.ApplyInstances(ctx, modulert.InstanceSet{Instances: []modulert.InstanceSpec{spec("slow", `{"label":"s"}`)}})
	if _, err := framework.OpenSink(ctx, "svi.n.sample.recorder.slow", "in"); err == nil || err.Error() != "instance_starting" {
		t.Fatalf("open while starting = %v", err)
	}
	framework.ApplyInstances(ctx, modulert.InstanceSet{})
	close(r.realizeGate)
	time.Sleep(50 * time.Millisecond)
	if statuses := framework.Instances(); len(statuses) != 0 {
		t.Fatalf("withdrawn instance came back: %+v", statuses)
	}
}

func TestValidateInstance(t *testing.T) {
	framework := sampleFramework(t, newRecorder())
	ctx := context.Background()
	if status, _ := framework.ValidateInstance(ctx, spec("ok", `{"label":"ok"}`)); status != 200 {
		t.Fatalf("valid = %d", status)
	}
	status, response := framework.ValidateInstance(ctx, spec("x", `{"label":"forbidden","typo":1}`))
	if status != 422 || len(response.Errors) == 0 {
		t.Fatalf("invalid = %d %+v", status, response)
	}
	if status, _ := framework.ValidateInstance(ctx, modulert.InstanceSpec{Name: "x", Type: "nope"}); status != 404 {
		t.Fatalf("unknown type = %d", status)
	}
}

func TestInstallRefusesASecondOwner(t *testing.T) {
	cfg := modulesdk.Config{SVISink: func(context.Context, string, string) (modulesdk.SVISink, error) { return nil, nil }}
	if err := resourcize.New("m").Install(&cfg); err == nil {
		t.Fatal("installed over an existing sink")
	}
}
