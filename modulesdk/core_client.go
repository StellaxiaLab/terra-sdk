package modulesdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
)

// ErrCoreUnavailable means the host offers no Core capability plane to this
// module: the host endpoint environment variable is absent, the endpoint cannot
// be reached, or the host answered that no invoker (or no such capability) is
// wired. A module treats it as "the host does not do this", not as a retryable
// fault of its own making.
var ErrCoreUnavailable = errors.New("modulesdk: host core capability plane is unavailable")

// defaultCoreInvokeTimeout bounds one Core invocation when the invocation
// carries no timeout of its own; the host is a loopback neighbour.
const defaultCoreInvokeTimeout = 10 * time.Second

// CoreClient invokes host Core capabilities on behalf of a hosted module
// (design §14.3: Module SDK → Host Capability Broker → Host Adapter → host
// core). It POSTs CoreInvocations to the host's capability endpoint,
// authenticated by the module's workload credential. Permission comes from the
// module's own manifest (permissions.coreOperations) — an undeclared operation
// is refused by the host broker as modulert.ErrCoreOperationDenied.
type CoreClient struct {
	endpoint   string
	credential string
	client     *http.Client
}

// CoreFromEnv builds a CoreClient from the environment the host launched this
// module with: the host capability endpoint (modulert.EnvHostEndpoint) and the
// workload credential. It returns ErrCoreUnavailable when the host injected no
// endpoint — a host without a Core capability plane simply doesn't set it.
func CoreFromEnv() (*CoreClient, error) {
	endpoint := strings.TrimSpace(os.Getenv(modulert.EnvHostEndpoint))
	if endpoint == "" {
		return nil, fmt.Errorf("%w: %s is not set", ErrCoreUnavailable, modulert.EnvHostEndpoint)
	}
	credential := os.Getenv(modulert.EnvCredential)
	if credential == "" {
		return nil, errors.New("modulesdk: workload credential is not set")
	}
	return NewCoreClient(endpoint, credential, nil), nil
}

// NewCoreClient builds a CoreClient against an explicit endpoint and credential
// (tests, custom wiring). A nil http.Client uses a bounded default.
func NewCoreClient(endpoint, credential string, client *http.Client) *CoreClient {
	if client == nil {
		client = &http.Client{Timeout: defaultCoreInvokeTimeout}
	}
	return &CoreClient{endpoint: endpoint, credential: credential, client: client}
}

// coreErrorEnvelope is the host's refusal shape, shared with module refusals:
// {"error":{"code","message"}}.
type coreErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Invoke executes one Core capability invocation and returns the host's result.
// Refusals map to sentinel errors a module can branch on:
//   - modulert.ErrCoreOperationDenied — the module's manifest does not declare
//     the operation (the broker refused; the operation never executed), or the
//     host could not verify the caller.
//   - ErrCoreUnavailable — the host offers no invoker, no such operation, or an
//     incompatible version.
//
// Any other non-2xx answer surfaces as an ordinary error with the host's
// message.
func (c *CoreClient) Invoke(ctx context.Context, invocation modulert.CoreInvocation) (modulert.CoreResult, error) {
	if strings.TrimSpace(invocation.OperationID) == "" {
		return modulert.CoreResult{}, errors.New("modulesdk: core invocation operationId is required")
	}
	encoded, err := json.Marshal(invocation)
	if err != nil {
		return modulert.CoreResult{}, err
	}
	if invocation.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(invocation.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.endpoint+modulert.CoreInvokePath, bytes.NewReader(encoded))
	if err != nil {
		return modulert.CoreResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set(modulert.CredentialHeader, c.credential)
	response, err := c.client.Do(request)
	if err != nil {
		return modulert.CoreResult{}, fmt.Errorf("%w: %v", ErrCoreUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		var result modulert.CoreResult
		if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result); err != nil {
			return modulert.CoreResult{}, fmt.Errorf("modulesdk: decode core result: %w", err)
		}
		return result, nil
	}

	payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	var refusal coreErrorEnvelope
	_ = json.Unmarshal(payload, &refusal)
	message := refusal.Error.Message
	if message == "" {
		message = strings.TrimSpace(string(payload))
	}
	switch response.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		return modulert.CoreResult{}, fmt.Errorf("%w: %s", modulert.ErrCoreOperationDenied, message)
	case http.StatusNotFound, http.StatusNotImplemented:
		return modulert.CoreResult{}, fmt.Errorf("%w: %s", ErrCoreUnavailable, message)
	default:
		return modulert.CoreResult{}, fmt.Errorf("modulesdk: core invocation failed with status %d: %s", response.StatusCode, message)
	}
}
