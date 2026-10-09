package modulert

import "encoding/json"

// Subset of Terra products/common/packages/terra-module-runtime/hostadapter.go:
// only the types a module sends to and reads from the host. The HostAdapter
// interface and the node catalog types are host-side and are not part of this SDK.

// CoreInvocation is a module's request to invoke a versioned Core capability
// through the Host Adapter (design §14.3). The module names the capability by its
// operation id and version range only — never an internal port, path or token.
type CoreInvocation struct {
	OperationID  string          `json:"operationId"`
	VersionRange string          `json:"versionRange,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	TimeoutMS    int             `json:"timeoutMs,omitempty"`
}

// CoreResult is the Host Adapter's reply to a CoreInvocation.
type CoreResult struct {
	Output json.RawMessage `json:"output,omitempty"`
}
