package conformance

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
)

// Identity is what a host issues to one launch of a module: the values behind
// the six environment variables.
type Identity struct {
	ModuleID   string
	InstanceID string
	Epoch      uint64
	Nonce      string
	Credential string
	// Endpoint is the loopback host:port the module is told to bind.
	Endpoint string
}

// NewIdentity issues a fresh identity for moduleID at the given epoch,
// reserving an ephemeral loopback port for the module to bind. Every launch
// gets new random values, as the contract requires of a host.
func NewIdentity(moduleID string, epoch uint64) (Identity, error) {
	endpoint, err := allocateLoopback()
	if err != nil {
		return Identity{}, err
	}
	instanceID, err := token(12)
	if err != nil {
		return Identity{}, err
	}
	nonce, err := token(24)
	if err != nil {
		return Identity{}, err
	}
	credential, err := token(24)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		ModuleID:   moduleID,
		InstanceID: instanceID,
		Epoch:      epoch,
		Nonce:      nonce,
		Credential: credential,
		Endpoint:   endpoint,
	}, nil
}

// Env returns the six KEY=VALUE pairs a host injects into the module process.
func (id Identity) Env() []string {
	return []string{
		EnvModuleID + "=" + id.ModuleID,
		EnvInstanceID + "=" + id.InstanceID,
		EnvEpoch + "=" + strconv.FormatUint(id.Epoch, 10),
		EnvNonce + "=" + id.Nonce,
		EnvCredential + "=" + id.Credential,
		EnvEndpoint + "=" + id.Endpoint,
	}
}

func allocateLoopback() (string, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", "0"))
	if err != nil {
		return "", fmt.Errorf("allocate loopback endpoint: %w", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("release loopback endpoint: %w", err)
	}
	return addr, nil
}

func token(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
