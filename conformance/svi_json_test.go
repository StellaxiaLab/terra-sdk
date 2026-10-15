package conformance_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/StellaxiaLab/terra-sdk/compat"
	"github.com/StellaxiaLab/terra-sdk/conformance"
	"github.com/StellaxiaLab/terra-sdk/modulert"
)

type sviFake struct {
	fakeModule
	resourcesNoAuth bool
	resourcesBody   string
	sinkNoAuth      bool
}

func (m sviFake) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", m.fakeModule.handler())
	authed := func(skip bool, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !skip && r.Header.Get(conformance.CredentialHeader) != m.id.Credential {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc(conformance.SVIResourcesPath, authed(m.resourcesNoAuth, func(w http.ResponseWriter, _ *http.Request) {
		body := m.resourcesBody
		if body == "" {
			body = "[]"
		}
		_, _ = w.Write([]byte(body))
	}))
	mux.HandleFunc(conformance.SVISinkPath, authed(m.sinkNoAuth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	return mux
}

func checkSVIFake(t *testing.T, mutate func(*sviFake)) conformance.Report {
	t.Helper()
	id, err := conformance.NewIdentity(sampleID, 1)
	if err != nil {
		t.Fatal(err)
	}
	module := sviFake{fakeModule: fakeModule{id: id}}
	if mutate != nil {
		mutate(&module)
	}
	server := httptest.NewUnstartedServer(module.handler())
	listener, err := newListener(id.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return conformance.Check(context.Background(), id, conformance.CheckOptions{SVI: true, CoreInvoke: true})
}

func TestSVIRulesAcceptAConformingModule(t *testing.T) {
	report := checkSVIFake(t, nil)
	if !report.OK() {
		t.Fatalf("rejected:\n%s", report)
	}
	counts := report.Counts()
	if counts[conformance.StatusSkip] != 2 {
		t.Fatalf("want 2 skips (sink bodies, core invoke), got %v\n%s", counts, report)
	}
	if !strings.Contains(report.String(), "SKIP core invoke") {
		t.Fatalf("core invoke skip missing:\n%s", report)
	}
}

func TestSVIRulesCatchViolations(t *testing.T) {
	cases := map[string]struct {
		mutate func(*sviFake)
		want   string
	}{
		"resources without credential check": {func(m *sviFake) { m.resourcesNoAuth = true }, "FAIL 401 without credential: " + conformance.SVIResourcesPath},
		"sink without credential check":      {func(m *sviFake) { m.sinkNoAuth = true }, "FAIL 401 without credential: " + conformance.SVISinkPath},
		"resources is not an array":          {func(m *sviFake) { m.resourcesBody = `{"a":1}` }, "FAIL svi resources are valid"},
		"descriptor is invalid":              {func(m *sviFake) { m.resourcesBody = `[{"resource_id":""}]` }, "FAIL svi resources are valid"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			report := checkSVIFake(t, tc.mutate)
			if report.OK() {
				t.Fatalf("not detected:\n%s", report)
			}
			if !strings.Contains(report.String(), tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, report)
			}
		})
	}
}

func TestSVIRulesAreOffByDefault(t *testing.T) {
	if report := checkFake(t, nil); strings.Contains(report.String(), "svi") {
		t.Fatalf("svi rules ran without the flag:\n%s", report)
	}
}

func TestWarnAndSkipDoNotFailTheReport(t *testing.T) {
	report := conformance.Report{Results: []conformance.Result{
		{Name: "a"},
		{Name: "b", Status: conformance.StatusWarn, Err: context.DeadlineExceeded},
		{Name: "c", Status: conformance.StatusSkip, Err: context.Canceled},
	}}
	if !report.OK() {
		t.Fatal("warn/skip must not fail")
	}
	report.Results = append(report.Results, conformance.Result{Name: "d", Err: context.Canceled})
	if report.OK() {
		t.Fatal("fail must fail")
	}
	onlySkip := conformance.Report{Results: []conformance.Result{{Name: "c", Status: conformance.StatusSkip}}}
	if onlySkip.OK() {
		t.Fatal("a report with nothing passed is not OK")
	}
}

func TestJSONReportShape(t *testing.T) {
	report := checkFake(t, func(m *fakeModule) { m.notReady = true })
	raw, err := json.Marshal(report.ToJSON(nil))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["schemaVersion"] != float64(1) || got["ok"] != false || got["wireContract"] != float64(1) {
		t.Fatalf("header: %s", raw)
	}
	results := got["results"].([]any)
	seenFail := false
	for _, r := range results {
		item := r.(map[string]any)
		if item["name"] == nil || item["status"] == nil {
			t.Fatalf("item missing fields: %v", item)
		}
		if item["status"] == "fail" {
			seenFail = true
			if item["error"] == nil {
				t.Fatalf("fail without error: %v", item)
			}
		}
	}
	if !seenFail {
		t.Fatalf("no fail in %s", raw)
	}
	harness := report.ToJSON(context.Canceled)
	if harness.OK || harness.Error == "" {
		t.Fatal("harness error must clear ok and be recorded")
	}
}

func TestKitOptionalPathsAndVersionMatchTheSDK(t *testing.T) {
	if conformance.SVIResourcesPath != modulert.SVIResourcesPath ||
		conformance.SVISinkPath != modulert.SVISinkPath ||
		conformance.CoreInvokePath != modulert.CoreInvokePath {
		t.Error("optional path literals drifted from modulert")
	}
	if conformance.WireContractVersion != compat.ContractVersion {
		t.Errorf("kit checks wire contract %d, SDK declares %d", conformance.WireContractVersion, compat.ContractVersion)
	}
}
