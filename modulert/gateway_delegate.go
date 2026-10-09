package modulert

// The delegate door's wire shape (terra.gateway.delegate, A3 — 설계 §11.4).
//
// The door itself lives in the host (terra-module-host registers it as a Core
// operation); what a MODULE needs is only the shape of the invocation it makes
// and the answer it reads. That shape is a contract between two processes
// that never import each other — the host on one side, an extension module on
// the other — so it lives here, in the dependency-free package both already
// link, exactly as CoreInvocation does. A module that kept its own copy of
// these field names would be a module whose door stops answering the day a
// name changes on one side only.

import "encoding/json"

// GatewayDelegateOperationID is the door's id — one name on every host.
const GatewayDelegateOperationID = "terra.gateway.delegate"

// GatewayDelegateOperationVersion is the version both hosts register.
const GatewayDelegateOperationVersion = "1.0.0"

// DelegatedCredentialPrefix marks a Gateway-issued delegated credential (an
// agent grant or an app scope token). It mirrors the Gateway's own
// scopeTokenPrefix; the two are pinned together by the module-gateway
// integration test, which pushes a credential the real Gateway issued through
// the door. The door carries credentials of this shape ONLY — a user session
// token is never relayed on a module's behalf (§15.3).
const DelegatedCredentialPrefix = "tsa_"

// TraceIDHeader is the response header the Gateway stamps with the trace id it
// issued for a request, and the one the door reads back off a relayed call.
//
// It lives here for the same reason the shapes above do: two processes that
// never import each other have to agree on the spelling. Without the agreement
// the trace id simply stops arriving — silently, because a missing header is
// indistinguishable from a Gateway that did not set one — and a delegated call
// becomes a line in the agent's record that nothing in the audit trail can be
// matched to.
const TraceIDHeader = "X-Terra-Trace-Id"

// GatewayDelegateInput is the module-facing input: one Gateway request, made
// as the presented credential. It is shaped like agentcore's Transport so a
// module can link that package and implement Transport by calling the door.
type GatewayDelegateInput struct {
	Credential string          `json:"credential"`
	Method     string          `json:"method"`
	Path       string          `json:"path"`
	Body       json.RawMessage `json:"body,omitempty"`
	TimeoutMS  int             `json:"timeout_ms,omitempty"`
}

// GatewayDelegateOutput is the Gateway's answer, status and body, carried
// whole. A Gateway refusal (403, 404, 429 …) is an OUTPUT, not an error: the
// error envelope is exactly what the caller needs to read, and a door that
// turned it into its own failure would erase the code. Errors are reserved for
// the door itself — a credential it will not carry, a path off the operation
// surface, a caller it cannot verify.
type GatewayDelegateOutput struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body,omitempty"`
	// TraceID is the Gateway's own id for this call, read off the relayed
	// response. It is the ONLY response header that crosses the door: a module
	// has no business knowing what else the Gateway said to it, and this one
	// exists so that what the agent recorded and what the Gateway audited can
	// be shown to be the same call. A refusal carries the same id inside its
	// error envelope; a success carried nothing at all before this field.
	TraceID string `json:"trace_id,omitempty"`
}
