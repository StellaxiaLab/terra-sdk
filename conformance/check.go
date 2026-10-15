package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Status is the level of one rule's outcome.
type Status string

const (
	// StatusPass: the rule held.
	StatusPass Status = "pass"
	// StatusFail: the rule was broken. Fails the report.
	StatusFail Status = "fail"
	// StatusWarn: a finding that does not fail the report (a rule being
	// phased in, see docs/policy/versioning.md section 5).
	StatusWarn Status = "warn"
	// StatusSkip: the rule was not checked; the reason is in Result.Err.
	StatusSkip Status = "skip"
)

// Result is the outcome of one contract rule.
//
// Level is derived when Status is empty: Err == nil means pass, otherwise
// fail. A result with Status warn or skip carries its message in Err but does
// not fail the report.
type Result struct {
	Name string
	Err  error
	// Status overrides the derived level (warn, skip). Empty = derived.
	Status Status
	// Rule is a stable machine-readable id for the rule (optional).
	Rule string
}

// Level returns the effective status of the result.
func (r Result) Level() Status {
	if r.Status != "" {
		return r.Status
	}
	if r.Err != nil {
		return StatusFail
	}
	return StatusPass
}

// Report lists every rule that was checked, in order.
type Report struct {
	Results []Result
}

// OK reports whether no rule failed and at least one rule passed.
// Warnings and skips do not fail a report.
func (r Report) OK() bool {
	passed := false
	for _, result := range r.Results {
		switch result.Level() {
		case StatusFail:
			return false
		case StatusPass:
			passed = true
		}
	}
	return passed
}

// String renders one PASS/FAIL/WARN/SKIP line per rule.
func (r Report) String() string {
	var out strings.Builder
	for _, result := range r.Results {
		switch result.Level() {
		case StatusFail:
			fmt.Fprintln(&out, "FAIL "+result.Name+": "+fmt.Sprint(result.Err))
		case StatusWarn:
			fmt.Fprintln(&out, "WARN "+result.Name+": "+fmt.Sprint(result.Err))
		case StatusSkip:
			fmt.Fprintln(&out, "SKIP "+result.Name+": "+fmt.Sprint(result.Err))
		default:
			fmt.Fprintln(&out, "PASS "+result.Name)
		}
	}
	return out.String()
}

func (r *Report) add(name string, err error) {
	r.Results = append(r.Results, Result{Name: name, Err: err})
}

func (r *Report) addSkip(name, reason string) {
	r.Results = append(r.Results, Result{Name: name, Status: StatusSkip, Err: errors.New(reason)})
}

// CheckOptions tunes Check.
type CheckOptions struct {
	// OperationPath is one of the module's own routes (for example
	// /api/modules/<id>/v1/status). When set, the kit also verifies that the
	// route demands the credential and answers 200 with it. Empty skips the rule.
	OperationPath string
	// RequestTimeout bounds each HTTP request. Default 5s.
	RequestTimeout time.Duration
	// Client is the HTTP client to use. Default: a fresh http.Client.
	Client *http.Client
	// SVI also checks the optional SVI control paths (/terra/svi/resources and
	// /terra/svi/sink). Enable it only for a module whose manifest declares
	// contributions.svi.
	SVI bool
	// CoreInvoke reports the /terra/core/invoke rule. That path is served by
	// the host, not the module, so the kit can only record it as skipped.
	CoreInvoke bool
}

// Check plays the host against a module that is already listening on
// id.Endpoint and verifies the contract's rules (§3–§5 of the contract):
//
//   - handshake echoes the issued instanceId and nonce
//   - readiness answers {"ready": true}
//   - a missing or wrong credential gets 401 and leaks neither value
//   - the module's own route (optional) is guarded by the same credential
func Check(ctx context.Context, id Identity, opts CheckOptions) Report {
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 5 * time.Second
	}
	if opts.Client == nil {
		opts.Client = &http.Client{}
	}
	c := checker{ctx: ctx, id: id, opts: opts}
	var report Report

	report.add("handshake echoes instanceId and nonce", c.handshake())
	report.add("readiness answers ready", c.readiness())
	for _, path := range []string{HandshakePath, ReadinessPath} {
		report.add("401 without credential: "+path, c.rejects(path, ""))
		report.add("401 with wrong credential: "+path, c.rejects(path, id.Credential+"x"))
	}
	if opts.OperationPath != "" {
		report.add("401 without credential: "+opts.OperationPath, c.rejects(opts.OperationPath, ""))
		report.add("200 with credential: "+opts.OperationPath, c.operation())
	}
	if opts.SVI {
		c.checkSVI(&report)
	}
	if opts.CoreInvoke {
		report.addSkip("core invoke: "+CoreInvokePath, "module-to-host direction: the host serves this path, and the kit does not play a capability broker")
	}
	return report
}

type checker struct {
	ctx  context.Context
	id   Identity
	opts CheckOptions
}

func (c checker) get(path, credential string) (int, []byte, error) {
	return c.do(http.MethodGet, path, credential, nil)
}

func (c checker) do(method, path, credential string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(c.ctx, c.opts.RequestTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.id.Endpoint+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if credential != "" {
		req.Header.Set(CredentialHeader, credential)
	}
	resp, err := c.opts.Client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

func (c checker) handshake() error {
	status, body, err := c.get(HandshakePath, c.id.Credential)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("status %d, want 200", status)
	}
	var reply struct {
		InstanceID string `json:"instanceId"`
		Nonce      string `json:"nonce"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return fmt.Errorf("body is not the handshake JSON: %w", err)
	}
	if reply.InstanceID != c.id.InstanceID || reply.Nonce != c.id.Nonce {
		return fmt.Errorf("identity mismatch: got instanceId=%q nonce=%q; the issued values must be echoed, not regenerated", reply.InstanceID, reply.Nonce)
	}
	return nil
}

func (c checker) readiness() error {
	status, body, err := c.get(ReadinessPath, c.id.Credential)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("status %d, want 200", status)
	}
	var reply struct {
		Ready  bool   `json:"ready"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return fmt.Errorf("body is not the readiness JSON: %w", err)
	}
	if !reply.Ready {
		return fmt.Errorf("ready=false (%s)", reply.Detail)
	}
	return nil
}

// rejects asks for path with the given credential ("" = none) and expects 401
// with a body that does not carry the issued secrets.
func (c checker) rejects(path, credential string) error {
	return c.rejectsWith(http.MethodGet, path, credential, nil)
}

func (c checker) rejectsWith(method, path, credential string, payload []byte) error {
	status, body, err := c.do(method, path, credential, payload)
	if err != nil {
		return err
	}
	if status != http.StatusUnauthorized {
		return fmt.Errorf("status %d, want 401", status)
	}
	for _, secret := range []string{c.id.Nonce, c.id.Credential, c.id.InstanceID} {
		if strings.Contains(string(body), secret) {
			return fmt.Errorf("401 body leaks an issued value")
		}
	}
	return nil
}

func (c checker) operation() error {
	status, body, err := c.get(c.opts.OperationPath, c.id.Credential)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("status %d, want 200: %s", status, strings.TrimSpace(string(body)))
	}
	return nil
}
