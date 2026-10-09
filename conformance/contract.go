package conformance

// The contract, spelled out on purpose. These literals are NOT imported from
// the SDK's modulert package: the kit must keep working for a module that has
// never seen that package, and a test (contract_drift_test.go) pins the two
// copies together so a rename on one side turns red here first.
const (
	EnvModuleID   = "TERRA_MODULE_ID"
	EnvInstanceID = "TERRA_INSTANCE_ID"
	EnvEpoch      = "TERRA_ACTIVATION_EPOCH"
	EnvNonce      = "TERRA_BOOTSTRAP_NONCE"
	EnvCredential = "TERRA_WORKLOAD_CREDENTIAL"
	EnvEndpoint   = "TERRA_LOOPBACK_ENDPOINT"

	// CredentialHeader carries the workload credential on every request the
	// host makes to the module.
	CredentialHeader = "X-Terra-Workload-Credential"

	HandshakePath = "/terra/handshake"
	ReadinessPath = "/terra/readiness"
)
