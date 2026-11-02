package resourcize_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/StellaxiaLab/terra-sdk/conformance"
	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
	"github.com/StellaxiaLab/terra-sdk/modulesdk"
)

// breakage rewrites one answer of a correct module so the kit sees a module
// that gets one rule wrong.
type breakage func(path string, request []byte, response *http.Response, body []byte) []byte

// brokenModule serves the sample framework behind a proxy on the endpoint the
// kit was told, with one rule broken.
func brokenModule(t *testing.T, broken breakage) conformance.Report {
	t.Helper()
	framework := sampleFramework(t, newRecorder())
	inner, err := conformance.NewIdentity("lab.example.recorder", 1)
	if err != nil {
		t.Fatal(err)
	}
	cfg := modulesdk.Config{Identity: modulert.WorkloadIdentity{
		ModuleID: inner.ModuleID, InstanceID: inner.InstanceID, Epoch: inner.Epoch, Nonce: inner.Nonce, Credential: inner.Credential, Endpoint: inner.Endpoint,
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

	outer, err := conformance.NewIdentity("lab.example.recorder", 1)
	if err != nil {
		t.Fatal(err)
	}
	// The kit talks to the proxy with the inner identity's secrets.
	outer.InstanceID, outer.Nonce, outer.Credential = inner.InstanceID, inner.Nonce, inner.Credential
	target, _ := url.Parse("http://" + inner.Endpoint)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var mu sync.Mutex
	proxy.ModifyResponse = func(response *http.Response) error {
		body, _ := io.ReadAll(response.Body)
		mu.Lock()
		request, _ := response.Request.Context().Value(requestBodyKey{}).([]byte)
		body = broken(response.Request.URL.Path, request, response, body)
		mu.Unlock()
		response.Body = io.NopCloser(bytes.NewReader(body))
		response.ContentLength = int64(len(body))
		response.Header.Del("Content-Length")
		return nil
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestBodyKey{}, body)))
	})
	front := httptest.NewUnstartedServer(handler)
	listener, err := newListener(outer.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	front.Listener = listener
	front.Start()
	t.Cleanup(front.Close)

	return conformance.Check(context.Background(), outer, conformance.CheckOptions{Resourcizer: &conformance.ResourcizerPlan{
		Samples: []conformance.InstanceSample{{Name: "front", Type: "sample.recorder", Config: json.RawMessage(`{"label":"front","token":"s3cret-token"}`)}},
		Invalid: []conformance.InstanceSample{{Name: "bad", Type: "sample.recorder", Config: json.RawMessage(`{"mode":"loud"}`)}},
	}})
}

type requestBodyKey struct{}

func failed(report conformance.Report, rule string) bool {
	for _, result := range report.Results {
		if strings.Contains(result.Name, rule) && result.Level() == conformance.StatusFail {
			return true
		}
	}
	return false
}

func TestConformanceCatchesEachBrokenRule(t *testing.T) {
	cases := map[string]struct {
		rule   string
		broken breakage
	}{
		"re-applied set gets a fresh time": {"status time moves only with the state", func(path string, _ []byte, _ *http.Response, body []byte) []byte {
			if path != modulert.SVIInstancesPath {
				return body
			}
			var result modulert.InstanceSetResult
			if json.Unmarshal(body, &result) != nil {
				return body
			}
			for index := range result.Instances {
				// A counter, not the clock: Windows' clock can return the
				// same instant for two calls in a row.
				applyCalls++
				result.Instances[index].UpdatedAt = time.Unix(int64(applyCalls), 0)
			}
			out, _ := json.Marshal(result)
			return out
		}},
		"second close fails": {"close twice", func(path string, request []byte, response *http.Response, body []byte) []byte {
			if path == modulert.SVISinkPath && bytes.Contains(request, []byte(`"op":"close"`)) && response.StatusCode == 200 {
				if closedOnce[string(request)] {
					response.StatusCode = http.StatusNotFound
				}
				closedOnce[string(request)] = true
			}
			return body
		}},
		"secret echoed": {"secret values", func(path string, _ []byte, _ *http.Response, body []byte) []byte {
			if path == modulert.SVIResourcizersPath {
				return bytes.Replace(body, []byte(`"Recorder"`), []byte(`"Recorder s3cret-token"`), 1)
			}
			return body
		}},
		"unstable etag": {"drawable and stable", func(path string, _ []byte, _ *http.Response, body []byte) []byte {
			if path == modulert.SVIResourcizersPath {
				etagCalls++
				return bytes.Replace(body, []byte(`"etag":"`), []byte(`"etag":"`+strings.Repeat("x", etagCalls)), 1)
			}
			return body
		}},
		"withdrawn sink still writes": {"withdrawing the instance", func(path string, request []byte, response *http.Response, body []byte) []byte {
			if path == modulert.SVISinkPath && bytes.Contains(request, []byte(`"op":"write"`)) && response.StatusCode != 200 && !bytes.Contains(request, []byte(`"sequence":2`)) {
				response.StatusCode = http.StatusOK
				return []byte(`{}`)
			}
			return body
		}},
	}
	for name, tc := range cases {
		closedOnce = map[string]bool{}
		etagCalls, applyCalls = 0, 0
		report := brokenModule(t, tc.broken)
		if !failed(report, tc.rule) {
			t.Errorf("%s: the kit did not fail %q:\n%s", name, tc.rule, report.String())
		}
	}
}

var (
	closedOnce = map[string]bool{}
	etagCalls  int
	applyCalls int
)
