package protocol

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The delegation intents the hosted access service needs (ADR-ID-001).
//
// All three are control instructions: the Tree that receives one answers it
// itself instead of re-originating it onto nodes, and it answers only when the
// instruction names it — the same rule IntentTreeDescribe follows, so a request
// that took a wrong turn is refused rather than served by whoever got it.
//
// What makes them different from every intent before them is where the
// authority comes from. A child Tree never authorizes against its parent's
// actor (DelegationOrigin is provenance only), and these intents do not change
// that. The two that act for a person carry that person's Terra ID assertion,
// audienced to the receiving Tree, and the receiving Tree verifies it against
// its own trust list and maps it to its own local user. The parent is the
// courier, not the principal.
//
// They are deliberately NOT in Terra's ControlInstructionIntent yet. The Master's
// control-instruction switch has no default branch, so an intent listed there
// without a handler would be answered "accepted" with an empty Detail. The part
// that writes the handlers adds them to that list in the same change.

const (
	// IntentNodeEnrollCodeIssue asks a Tree to mint one node enrollment code on
	// behalf of the person whose assertion it carries.
	IntentNodeEnrollCodeIssue = "node.enroll_code.issue"
	// IntentNodeEnrollExchange forwards a leaf's code exchange to the Tree that
	// issued the code. Reserved: its answer carries the new node's device
	// token through the forwarding Tree, and whether that is acceptable is an
	// open question in ADR-ID-001.
	IntentNodeEnrollExchange = "node.enroll.exchange"
	// IntentExternalIdentityRevoke asks a Tree to suspend its link to one
	// external account, on the word of the issuer of that account.
	IntentExternalIdentityRevoke = "external_identity.revoke"
)

// MaxDelegatedEnrollCodeTTLSec caps how long a code minted through delegation
// may live. It matches the Master's own ceiling for a node code; a delegated
// request must not be able to ask for more than a local one could.
const MaxDelegatedEnrollCodeTTLSec = 900

// HostedAccessIntent reports whether an intent is one of the three above.
func HostedAccessIntent(intent string) bool {
	switch intent {
	case IntentNodeEnrollCodeIssue, IntentNodeEnrollExchange, IntentExternalIdentityRevoke:
		return true
	}
	return false
}

// NodeEnrollCodeIssueInstruction is the payload of IntentNodeEnrollCodeIssue.
type NodeEnrollCodeIssueInstruction struct {
	// TreeNodeID is the Tree being asked. Any other Tree refuses.
	TreeNodeID string `json:"tree_node_id"`
	// Assertion is a Terra ID assertion with terra_use node.enroll_code.issue
	// and aud tree:<TreeNodeID>. It is the only authority this request has.
	Assertion string `json:"assertion"`
	// ParentNodeID places the node the code creates. Empty means the root.
	ParentNodeID string `json:"parent_node_id,omitempty"`
	// Label is the person's note about which machine this is for.
	Label string `json:"label,omitempty"`
	// MasterURL is the address the code carries, and here it is required.
	// A request that arrived through delegation has no origin a leaf could
	// reach, so the Tree's usual fallback (the request's own origin) would bake
	// an unreachable address into the code.
	MasterURL string `json:"master_url"`
	// ExpiresInSec is 0 for the Tree's default, at most
	// MaxDelegatedEnrollCodeTTLSec.
	ExpiresInSec int `json:"expires_in_sec,omitempty"`
}

// NodeEnrollCodeIssueAnswer is its Detail. Code is the plaintext and this is
// the only place it is ever readable, exactly as on the Tree's own issue route.
type NodeEnrollCodeIssueAnswer struct {
	Code         string    `json:"code"`
	CodeID       string    `json:"code_id"`
	MasterURL    string    `json:"master_url"`
	ExpiresAt    time.Time `json:"expires_at"`
	ParentNodeID string    `json:"parent_node_id,omitempty"`
}

// NodeEnrollExchangeInstruction is the payload of IntentNodeEnrollExchange.
//
// It carries no assertion: the code is the credential, as it is on the Tree's
// own unauthenticated exchange route.
type NodeEnrollExchangeInstruction struct {
	TreeNodeID string `json:"tree_node_id"`
	// Enrollment is the leaf's request body, verbatim. The receiving Tree
	// validates it as if the leaf had sent it directly.
	Enrollment json.RawMessage `json:"enrollment"`
	// ClientAddress is where the leaf's request came from, for the receiving
	// Tree's audit line and rate limit. Never an authorization input.
	ClientAddress string `json:"client_address,omitempty"`
}

// NodeEnrollExchangeAnswer is its Detail: what the receiving Tree's exchange
// route would have answered, so the forwarding Tree can hand the leaf the same
// response.
type NodeEnrollExchangeAnswer struct {
	HTTPStatus int             `json:"http_status"`
	Body       json.RawMessage `json:"body"`
}

// ExternalIdentityRevokeInstruction is the payload of
// IntentExternalIdentityRevoke.
//
// The assertion is the issuer speaking about its own subject, so the receiving
// Tree needs no local user's permission to act on it — only its trust list.
type ExternalIdentityRevokeInstruction struct {
	TreeNodeID string `json:"tree_node_id"`
	// Assertion has terra_use external_identity.revoke, sub the account,
	// terra_reason, and aud tree:<TreeNodeID>.
	Assertion string `json:"assertion"`
}

// ExternalIdentityRevokeAnswer is its Detail. Suspended is true when this Tree
// held a link to the account and that link is now suspended.
type ExternalIdentityRevokeAnswer struct {
	Suspended bool `json:"suspended"`
}

// ValidateNodeEnrollCodeIssueInstruction checks the shape a receiving Tree may
// rely on before it verifies the assertion.
func ValidateNodeEnrollCodeIssueInstruction(instruction NodeEnrollCodeIssueInstruction) error {
	if err := validateHostedAccessTarget(instruction.TreeNodeID); err != nil {
		return err
	}
	if strings.TrimSpace(instruction.Assertion) == "" {
		return fmt.Errorf("%w: an enroll code issue must carry an assertion", ErrInvalidDelegation)
	}
	if parent := strings.TrimSpace(instruction.ParentNodeID); parent != "" && !ValidNodeID(parent) {
		return fmt.Errorf("%w: parent node id %q is not a node id", ErrInvalidDelegation, parent)
	}
	if !validEnrollMasterURL(instruction.MasterURL) {
		return fmt.Errorf("%w: master url must be an absolute http(s) origin a leaf can reach", ErrInvalidDelegation)
	}
	if instruction.ExpiresInSec < 0 || instruction.ExpiresInSec > MaxDelegatedEnrollCodeTTLSec {
		return fmt.Errorf("%w: expires_in_sec must be between 0 and %d", ErrInvalidDelegation, MaxDelegatedEnrollCodeTTLSec)
	}
	return nil
}

// CheckEnrollCodeIssueBinding refuses an instruction whose placement differs
// from the one its verified assertion pinned. An assertion without the pin
// binds nothing here; the person's own node.control still decides whether the
// placement is allowed at all.
func CheckEnrollCodeIssueBinding(claims TerraIDClaims, instruction NodeEnrollCodeIssueInstruction) error {
	if claims.ParentNodeID == nil {
		return nil
	}
	if pinned, asked := *claims.ParentNodeID, strings.TrimSpace(instruction.ParentNodeID); pinned != asked {
		return fmt.Errorf("%w: the assertion pins parent %q, the instruction asks for %q", ErrInvalidDelegation, pinned, asked)
	}
	return nil
}

// ValidateNodeEnrollExchangeInstruction checks the shape of a forwarded
// exchange. The enrollment body must be a JSON object; its fields are the
// receiving Tree's to judge.
func ValidateNodeEnrollExchangeInstruction(instruction NodeEnrollExchangeInstruction) error {
	if err := validateHostedAccessTarget(instruction.TreeNodeID); err != nil {
		return err
	}
	var body map[string]json.RawMessage
	if len(instruction.Enrollment) == 0 || json.Unmarshal(instruction.Enrollment, &body) != nil || body == nil {
		return fmt.Errorf("%w: enrollment must be a JSON object", ErrInvalidDelegation)
	}
	return nil
}

// ValidateExternalIdentityRevokeInstruction checks the shape of a revoke.
func ValidateExternalIdentityRevokeInstruction(instruction ExternalIdentityRevokeInstruction) error {
	if err := validateHostedAccessTarget(instruction.TreeNodeID); err != nil {
		return err
	}
	if strings.TrimSpace(instruction.Assertion) == "" {
		return fmt.Errorf("%w: a revoke must carry an assertion", ErrInvalidDelegation)
	}
	return nil
}

func validateHostedAccessTarget(treeNodeID string) error {
	if !ValidNodeID(treeNodeID) {
		return fmt.Errorf("%w: tree node id %q is not a node id", ErrInvalidDelegation, treeNodeID)
	}
	return nil
}

// validEnrollMasterURL accepts an origin a leaf could dial: http or https, a
// host, and nothing that would make two codes for the same Tree look different
// (credentials, query, fragment).
func validEnrollMasterURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

// TreeReachability is how a Tree says where it can be reached from, published
// in its subtree summary so a registry can grey out a Tree a device could not
// enroll into instead of letting the enrollment fail afterwards.
//
// It is the Tree's own statement about itself, never a measurement by anyone
// else, and it decides nothing: a Tree that says "public" and is not gets a
// failed exchange, exactly as today.
type TreeReachability string

const (
	// TreeReachabilityUnknown is the zero value: the Tree has not said.
	TreeReachabilityUnknown TreeReachability = ""
	// TreeReachabilityPublic means its Master URL is reachable from the
	// internet.
	TreeReachabilityPublic TreeReachability = "public"
	// TreeReachabilityLANOnly means only machines on its own network can reach
	// it.
	TreeReachabilityLANOnly TreeReachability = "lan_only"
	// TreeReachabilityMeshOnly means it is reachable over the Terra mesh and
	// nowhere else.
	TreeReachabilityMeshOnly TreeReachability = "mesh_only"
)

// Valid reports whether r is one of the values above, the zero value included.
func (r TreeReachability) Valid() bool {
	switch r {
	case TreeReachabilityUnknown, TreeReachabilityPublic, TreeReachabilityLANOnly, TreeReachabilityMeshOnly:
		return true
	}
	return false
}

// Error codes of the managed Tree's internal account entrance (P3 K11).
const (
	// ErrCodeIssuerNotTrusted (409): the issuer is not on this Master's trusted
	// external issuer list, so no account of it can be provisioned here.
	ErrCodeIssuerNotTrusted = "ISSUER_NOT_TRUSTED"
)

// ManagedAccountHandleMaxRunes bounds the optional handle used as a display name.
const ManagedAccountHandleMaxRunes = 80

// ManagedAccountProvisionRequest is the body of
// PUT /api/v1/internal/accounts/{account_id}: the registry asking the managed
// Tree to give a new Terra ID account a Master user. The account id is in the
// path; it is the external identity's subject.
type ManagedAccountProvisionRequest struct {
	// Issuer is the Terra ID issuer URL. It must be on the Tree's trust list.
	Issuer string `json:"issuer"`
	// Handle is optional and only names the user (display name).
	Handle string `json:"handle,omitempty"`
}

// ManagedAccountProvisionAnswer is the data of a successful provision.
type ManagedAccountProvisionAnswer struct {
	UserID    string `json:"user_id"`
	ClusterID string `json:"cluster_id"`
	// Created is false when the account was already provisioned.
	Created bool `json:"created"`
}

// ValidateManagedAccountProvision checks the shape of an account id and its
// request. It does not look at the trust list; that is the Tree's own state.
func ValidateManagedAccountProvision(accountID string, request ManagedAccountProvisionRequest) error {
	if !ValidTerraAccountID(accountID) {
		return fmt.Errorf("account_id must be acc_ followed by a 26-character uppercase ULID")
	}
	if strings.TrimSpace(request.Issuer) == "" {
		return fmt.Errorf("issuer is required")
	}
	if len([]rune(strings.TrimSpace(request.Handle))) > ManagedAccountHandleMaxRunes {
		return fmt.Errorf("handle is longer than %d characters", ManagedAccountHandleMaxRunes)
	}
	return nil
}
