package modulert

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// The identity/handshake environment keys form the wire contract the Runtime and
// terra-module-sdk share (design §12.1, §15.3/§15.4). The Runtime injects them
// into a workload via WorkloadIdentity.Env; the module reads them back with
// IdentityFromEnv. Keep the two sides in sync.
const (
	EnvModuleID   = "TERRA_MODULE_ID"
	EnvInstanceID = "TERRA_INSTANCE_ID"
	EnvEpoch      = "TERRA_ACTIVATION_EPOCH"
	EnvNonce      = "TERRA_BOOTSTRAP_NONCE"
	EnvCredential = "TERRA_WORKLOAD_CREDENTIAL"
	EnvEndpoint   = "TERRA_LOOPBACK_ENDPOINT"
	// EnvHostEndpoint is where the host serves its Core capability plane
	// (design §14.3): the loopback host:port a module POSTs CoreInvocations to
	// (CoreInvokePath), authenticated by the same workload credential. The host
	// injects it alongside the identity environment; it is absent when the host
	// offers no Core capabilities, so it is deliberately NOT part of
	// WorkloadIdentity or IdentityFromEnv's required set.
	EnvHostEndpoint = "TERRA_MODULE_HOST_ENDPOINT"
)

// LoopbackHost is the only interface a 안 A workload endpoint binds. Fixed ports
// are never declared in a manifest; the Runtime assigns an ephemeral loopback
// port and a short credential per launch (§15.4).
const LoopbackHost = "127.0.0.1"

// WorkloadIdentity is the short-lived subject the Runtime issues to one launched
// workload instance (design §15.3). It binds the instance to a bootstrap nonce
// the module echoes to prove it is the launched instance, and a credential the
// module requires on its loopback endpoint. It never carries a user Bearer or a
// long-lived device token, and is never reused across launches — even for the
// same module id a new instance gets a fresh identity.
type WorkloadIdentity struct {
	ModuleID   string
	InstanceID string
	Epoch      uint64
	Nonce      string
	Credential string
	// Endpoint is the loopback host:port the Runtime assigned to this instance.
	Endpoint string
}

// Env returns the environment variables to merge into a WorkloadSpec so the
// launched module can read its identity and endpoint. It is the counterpart of
// IdentityFromEnv.
func (id WorkloadIdentity) Env() map[string]string {
	return map[string]string{
		EnvModuleID:   id.ModuleID,
		EnvInstanceID: id.InstanceID,
		EnvEpoch:      strconv.FormatUint(id.Epoch, 10),
		EnvNonce:      id.Nonce,
		EnvCredential: id.Credential,
		EnvEndpoint:   id.Endpoint,
	}
}

// IdentityFromEnv reconstructs a WorkloadIdentity from the environment a hosted
// module was launched with (the counterpart of Env). lookup is typically
// os.Getenv. It errors if any required field is missing or the epoch is invalid.
func IdentityFromEnv(lookup func(string) string) (WorkloadIdentity, error) {
	epoch, err := strconv.ParseUint(strings.TrimSpace(lookup(EnvEpoch)), 10, 64)
	if err != nil {
		return WorkloadIdentity{}, fmt.Errorf("invalid %s: %w", EnvEpoch, err)
	}
	id := WorkloadIdentity{
		ModuleID:   strings.TrimSpace(lookup(EnvModuleID)),
		InstanceID: strings.TrimSpace(lookup(EnvInstanceID)),
		Epoch:      epoch,
		Nonce:      lookup(EnvNonce),
		Credential: lookup(EnvCredential),
		Endpoint:   strings.TrimSpace(lookup(EnvEndpoint)),
	}
	if id.ModuleID == "" || id.InstanceID == "" || id.Nonce == "" || id.Credential == "" || id.Endpoint == "" {
		return WorkloadIdentity{}, errors.New("incomplete workload identity in environment")
	}
	return id, nil
}

// IdentityIssuer mints WorkloadIdentity values. It is safe for concurrent use.
type IdentityIssuer struct{}

// NewIdentityIssuer returns an issuer.
func NewIdentityIssuer() *IdentityIssuer { return &IdentityIssuer{} }

// Issue mints a fresh identity for a module launch at the given activation epoch,
// assigning it endpoint (from AllocateLoopbackEndpoint). The instance id, nonce
// and credential are cryptographically random and unique to this launch.
func (*IdentityIssuer) Issue(moduleID string, epoch uint64, endpoint string) (WorkloadIdentity, error) {
	moduleID = strings.TrimSpace(moduleID)
	if moduleID == "" {
		return WorkloadIdentity{}, errors.New("module id is required to issue a workload identity")
	}
	if strings.TrimSpace(endpoint) == "" {
		return WorkloadIdentity{}, errors.New("loopback endpoint is required to issue a workload identity")
	}
	instanceID, err := randToken(12)
	if err != nil {
		return WorkloadIdentity{}, err
	}
	nonce, err := randToken(24)
	if err != nil {
		return WorkloadIdentity{}, err
	}
	credential, err := randToken(24)
	if err != nil {
		return WorkloadIdentity{}, err
	}
	return WorkloadIdentity{
		ModuleID:   moduleID,
		InstanceID: instanceID,
		Epoch:      epoch,
		Nonce:      nonce,
		Credential: credential,
		Endpoint:   endpoint,
	}, nil
}

// AllocateLoopbackEndpoint reserves a loopback host:port by binding an ephemeral
// port on 127.0.0.1 and releasing it, returning the address for the workload to
// bind. The bootstrap credential — not the port number — is what protects the
// endpoint, so the brief release window is acceptable in the 1차 scope (§15.4).
func AllocateLoopbackEndpoint() (string, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(LoopbackHost, "0"))
	if err != nil {
		return "", fmt.Errorf("allocate loopback endpoint: %w", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("release loopback endpoint: %w", err)
	}
	return addr, nil
}

func randToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
