package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Result is the outcome of one contract rule.
type Result struct {
	Name string
	Err  error
}

// Report lists every rule that was checked, in order.
type Report struct {
	Results []Result
}

// OK reports whether every rule passed.
func (r Report) OK() bool {
	for _, result := range r.Results {
		if result.Err != nil {
			return false
		}
	}
	return len(r.Results) > 0
}

// String renders one PASS/FAIL line per rule.
func (r Report) String() string {
	var out strings.Builder
	for _, result := range r.Results {
		if result.Err != nil {
			fmt.Fprintf(&out, "FAIL %s: %v\n", result.Name, result.Err)
		} else {
			fmt.Fprintf(&out, "PASS %s\n", result.Name)
		}
	}
	return out.String()
}

func (r *Report) add(name string, err error) {
	r.Results = append(r.Results, Result{Name: name, Err: err})
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
	return report
}

type checker struct {
	ctx  context.Context
	id   Identity
	opts CheckOptions
}

func (c checker) get(path, credential string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(c.ctx, c.opts.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.id.Endpoint+path, nil)
	if err != nil {
		return 0, nil, err
	}
	if credential != "" {
		req.Header.Set(CredentialHeader, credential)
	}
	resp, err := c.opts.Client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, err
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
	status, body, err := c.get(path, credential)
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
