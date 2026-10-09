package modulert

// Subset of Terra products/common/packages/terra-module-runtime/manifest.go:
// only the readiness contract Activate takes. The manifest is host-side and is
// not part of this SDK.

type Readiness struct {
	OperationID  string `json:"operationId,omitempty"`
	VersionRange string `json:"versionRange,omitempty"`
	TimeoutMS    int    `json:"timeoutMs,omitempty"`
}
