package protocol

// What a Tree accepts as proof that a person is who Terra ID says they are.
//
// A Tree has always been its own authority: it issues its own accounts, and a
// login is a password it checked itself. Hosted access adds a second way in — a
// token signed by an issuer the Tree's operator chose to trust — and the risk of
// that second way is entirely in how loosely it is read. A token that is valid
// for every Tree is a credential that one compromised Tree can replay against
// all the others; a token whose purpose is not pinned is a login that can be
// presented where an enrolment assertion was expected; an issuer that is
// compared by "looks like the same URL" is an issuer anyone can impersonate with
// a trailing slash.
//
// So this file is the reading rule, owned by the side that verifies
// (ADR-ID-001); Terra's terra-protocol re-exports it. The terra-id
// service imports these same functions to check its own output before it signs,
// which is what keeps the issuer and every verifier from drifting apart: there
// is one definition of a well-formed token, not one per repository.
//
// It defines the claim shape, the expectation a verifier brings, and the
// claim-level checks. Signatures and keys are in terraid_jws.go, the offline key
// bundle is in terraid_keybundle.go. Nothing here stores, fetches or caches —
// trust lists and JWKS refresh are the Tree's policy (P2), not the contract.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Token purposes, carried in the `terra_use` claim.
//
// A purpose is what stops one kind of token from being spent as another. The
// login token is the only one allowed to omit it, because a self-hosted Tree
// trusting its company IdP (architecture A-3) receives a plain OIDC id_token
// that has never heard of Terra; every other purpose is an assertion Terra ID
// minted for exactly one action and must say so.
const (
	// TerraIDUseTreeLogin is a person logging in to a Tree.
	TerraIDUseTreeLogin = "tree.login"
	// TerraIDUseNodeEnrollCodeIssue authorizes one enrol-code issuance on the
	// named Tree on the subject's behalf (IntentNodeEnrollCodeIssue).
	TerraIDUseNodeEnrollCodeIssue = "node.enroll_code.issue"
	// TerraIDUseExternalIdentityRevoke is the issuer telling a Tree that one of
	// its own subjects must be suspended (IntentExternalIdentityRevoke).
	TerraIDUseExternalIdentityRevoke = "external_identity.revoke"
)

// JWS `typ` header values.
//
// The two kinds are told apart in the header as well as in the claims, so a
// login presented where an assertion belongs (or the reverse) is refused before
// a single claim is read — RFC 8725 §3.11 explicit typing.
const (
	// TerraIDTokenType is the login token's typ. It may also be absent, which
	// is what a standard id_token from a foreign IdP usually looks like.
	TerraIDTokenType = "JWT"
	// TerraIDAssertionType is the required typ of every purpose assertion.
	TerraIDAssertionType = "terra-assertion+jwt"
)

// Authentication context classes, carried in `acr`.
//
// MFA is decided by acr alone. `amr` lists methods and is useful to show a
// person how they signed in, but a verifier that inferred "two factors" from a
// list of method names would be re-implementing the issuer's policy, badly, and
// differently in every Tree.
const (
	// TerraIDACRMultiFactor means the person proved more than one factor.
	TerraIDACRMultiFactor = "urn:terra:acr:mfa"
	// TerraIDACRSingleFactor means one factor.
	TerraIDACRSingleFactor = "urn:terra:acr:sfa"
)

// Revocation reasons, carried in `terra_reason`. Only an
// external_identity.revoke assertion carries one, and the reason is recorded,
// not acted on differently: every reason suspends the mapping.
const (
	TerraIDRevokeReasonAccountCompromised = "account_compromised"
	TerraIDRevokeReasonAccountDeleted     = "account_deleted"
	TerraIDRevokeReasonLoginMethodRemoved = "login_method_removed"
	TerraIDRevokeReasonOperator           = "operator"
)

// Time rules.
//
// The lifetime caps are on exp-iat, not on exp-now, so an issuer cannot mint a
// long-lived token by back-dating iat. A login token lives as long as a person
// plausibly takes to land on the Tree after signing in. An assertion is minted
// by the registry for one call it is about to make, so a minute is generous —
// except revoke, which is fanned out best-effort to Trees that may be slow to
// answer.
const (
	// TerraIDClockLeeway is the clock skew tolerated on every time check.
	TerraIDClockLeeway = 60 * time.Second
	// TerraIDLoginMaxLifetime caps exp-iat for a tree.login token.
	TerraIDLoginMaxLifetime = 600 * time.Second
	// TerraIDAssertionMaxLifetime caps exp-iat for a purpose assertion.
	TerraIDAssertionMaxLifetime = 60 * time.Second
	// TerraIDRevokeMaxLifetime caps exp-iat for external_identity.revoke.
	TerraIDRevokeMaxLifetime = 600 * time.Second
)

// TreeAudiencePrefix opens every audience: `tree:` + the Tree's own node_id.
// It reuses the identifier delegation and the hierarchy already call a Tree by,
// rather than minting a new one that would need its own registry.
const TreeAudiencePrefix = "tree:"

// Size limits on opaque claim values. They are bounds on what a verifier will
// store or log, not policy about what an issuer may choose.
const (
	terraIDMaxSubjectBytes = 255
	terraIDMaxJTIBytes     = 128
)

// Exchange entry error codes (POST /api/v1/auth/external/exchange, P2).
//
// They are deliberately coarse. A token from an issuer the Tree does not trust
// is EXTERNAL_TOKEN_INVALID like any other bad token, so the endpoint never
// tells a caller which issuers a Tree trusts.
const (
	// ErrCodeExternalTokenInvalid (401): signature, header or claim failure,
	// including an untrusted issuer.
	ErrCodeExternalTokenInvalid = "EXTERNAL_TOKEN_INVALID"
	// ErrCodeExternalIdentityNotLinked (403): no mapping for (iss, sub).
	ErrCodeExternalIdentityNotLinked = "EXTERNAL_IDENTITY_NOT_LINKED"
	// ErrCodeExternalIdentitySuspended (403): the mapping is suspended.
	ErrCodeExternalIdentitySuspended = "EXTERNAL_IDENTITY_SUSPENDED"
	// ErrCodeExternalMFARequired (403): the Tree requires MFA and acr is not
	// urn:terra:acr:mfa.
	ErrCodeExternalMFARequired = "EXTERNAL_MFA_REQUIRED"
)

// Terra ID failures.
var (
	// ErrTerraIDTokenInvalid is every reason a token or assertion is refused:
	// signature, header and claims alike. Callers map it to one answer
	// (ErrCodeExternalTokenInvalid); the wrapped text is for logs.
	ErrTerraIDTokenInvalid = errors.New("terra id token is invalid")
	// ErrTerraIDKeyInvalid is a JWK or JWK set that cannot be used to verify.
	ErrTerraIDKeyInvalid = errors.New("terra id key is invalid")
	// ErrTerraIDKeyBundleInvalid is an offline key bundle that must not be used.
	ErrTerraIDKeyBundleInvalid = errors.New("terra id key bundle is invalid")
	// ErrTreeAudienceInvalid is an audience that does not name one Tree.
	ErrTreeAudienceInvalid = errors.New("tree audience is invalid")
)

// TerraIDAudience is the `aud` claim.
//
// RFC 7519 lets it be a string or an array, and real issuers send both, so it
// is read either way. It is written as a plain string when it holds one value
// because that is the only shape a valid Terra ID token has, and the form every
// verifier understands.
type TerraIDAudience []string

// MarshalJSON writes a single audience as a string, anything else as an array.
func (a TerraIDAudience) MarshalJSON() ([]byte, error) {
	if len(a) == 1 {
		return json.Marshal(a[0])
	}
	return json.Marshal([]string(a))
}

// UnmarshalJSON reads a string or an array of strings.
func (a *TerraIDAudience) UnmarshalJSON(raw []byte) error {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		*a = TerraIDAudience{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return fmt.Errorf("aud must be a string or an array of strings")
	}
	*a = TerraIDAudience(many)
	return nil
}

// TerraIDActor is the RFC 8693 `act` claim: who carried the assertion here
// (for example `svc:terra-access`). It is recorded for audit and never
// authorizes anything — the subject is still the person.
type TerraIDActor struct {
	Subject string `json:"sub"`
}

// TerraIDClaims is the payload of a Terra ID token or assertion.
//
// Times are NumericDate seconds as on the wire; 0 means absent. There is no
// email claim on purpose: identities are mapped by (iss, sub) only, because an
// address is reassignable and a mapping keyed on it is a takeover waiting for
// someone to let a domain lapse (architecture H2).
type TerraIDClaims struct {
	Issuer    string          `json:"iss"`
	Subject   string          `json:"sub"`
	Audience  TerraIDAudience `json:"aud"`
	ExpiresAt int64           `json:"exp"`
	IssuedAt  int64           `json:"iat"`
	NotBefore int64           `json:"nbf,omitempty"`
	JWTID     string          `json:"jti,omitempty"`
	Use       string          `json:"terra_use,omitempty"`
	ACR       string          `json:"acr,omitempty"`
	// AMR is for display and audit. It is never an authorization input.
	AMR []string `json:"amr,omitempty"`
	// AuthTime is when the person actually authenticated, for a Tree that
	// requires recent re-authentication.
	AuthTime int64 `json:"auth_time,omitempty"`
	// Handle is display only. Handles change; mapping or authorizing on one
	// would follow the name instead of the person.
	Handle string        `json:"terra_handle,omitempty"`
	Actor  *TerraIDActor `json:"act,omitempty"`
	// Reason is set on external_identity.revoke and nowhere else.
	Reason string `json:"terra_reason,omitempty"`
	// ParentNodeID optionally pins where a code minted on a
	// node.enroll_code.issue assertion may place its node (architecture R-3).
	// Absent means unpinned; present and empty means the Tree's root. The
	// receiving Tree enforces it with CheckEnrollCodeIssueBinding.
	ParentNodeID *string `json:"terra_parent_node_id,omitempty"`
}

// MultiFactor reports whether the issuer says more than one factor was used.
// It reads acr only — see TerraIDACRMultiFactor.
func (c TerraIDClaims) MultiFactor() bool { return c.ACR == TerraIDACRMultiFactor }

// TerraIDExpectation is what a verifier brings to a token: which issuer it
// trusts, which Tree it is, which purpose it is accepting, and what time it is.
//
// Every field is required. A verifier that left one empty would be accepting
// "any", and an expectation is the one place where "any" must not be possible.
type TerraIDExpectation struct {
	// Issuer is the trust list's value, compared byte for byte.
	Issuer string
	// Audience is TreeAudience(this Tree's own node_id).
	Audience string
	// Use is TerraIDUseTreeLogin or one of the assertion purposes.
	Use string
	// Now is the verifier's clock.
	Now time.Time
	// RequireTerraIDSubject is true for the Terra ID profile, where sub must
	// be an acc_ account id. A foreign IdP's sub is an opaque string.
	RequireTerraIDSubject bool
}

// ExternalExchangeRequest is the body of POST /api/v1/auth/external/exchange.
type ExternalExchangeRequest struct {
	Token string `json:"token"`
}

// terraAccountIDPattern is `acc_` + a 26-character Crockford base32 ULID,
// uppercase, without the ambiguous I, L, O and U.
var terraAccountIDPattern = regexp.MustCompile(`^acc_[0-9A-HJKMNP-TV-Z]{26}$`)

// ValidTerraAccountID reports whether id is a Terra ID account id.
func ValidTerraAccountID(id string) bool { return terraAccountIDPattern.MatchString(id) }

// ValidIssuerURL reports whether value can be an issuer.
//
// The shape is narrow because issuers are compared byte for byte, and every
// freedom in the shape is a second spelling of the same issuer that a trust
// list would not match — or worse, would match by accident. So: an absolute
// https URL with a host, no user info, no query, no fragment and no trailing
// slash. Plain http is allowed only for a loopback host, which is a test
// issuer on the same machine and nothing else.
func ValidIssuerURL(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "?# \t\r\n") {
		return false
	}
	if strings.HasSuffix(value, "/") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" {
		return false
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		return loopbackHost(parsed.Hostname())
	default:
		return false
	}
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// TreeAudience is the audience a token for the Tree whose own node is nodeID
// must carry.
func TreeAudience(nodeID string) (string, error) {
	if nodeID != strings.TrimSpace(nodeID) || !ValidNodeID(nodeID) {
		return "", fmt.Errorf("%w: %q is not a node id", ErrTreeAudienceInvalid, nodeID)
	}
	return TreeAudiencePrefix + nodeID, nil
}

// ParseTreeAudience returns the node_id an audience names.
func ParseTreeAudience(aud string) (string, error) {
	nodeID, found := strings.CutPrefix(aud, TreeAudiencePrefix)
	if !found {
		return "", fmt.Errorf("%w: %q does not start with %q", ErrTreeAudienceInvalid, aud, TreeAudiencePrefix)
	}
	if _, err := TreeAudience(nodeID); err != nil {
		return "", err
	}
	return nodeID, nil
}

// terraIDMaxLifetime is the exp-iat cap for a purpose.
func terraIDMaxLifetime(use string) (time.Duration, bool) {
	switch use {
	case TerraIDUseTreeLogin:
		return TerraIDLoginMaxLifetime, true
	case TerraIDUseNodeEnrollCodeIssue:
		return TerraIDAssertionMaxLifetime, true
	case TerraIDUseExternalIdentityRevoke:
		return TerraIDRevokeMaxLifetime, true
	}
	return 0, false
}

func knownRevokeReason(reason string) bool {
	switch reason {
	case TerraIDRevokeReasonAccountCompromised, TerraIDRevokeReasonAccountDeleted,
		TerraIDRevokeReasonLoginMethodRemoved, TerraIDRevokeReasonOperator:
		return true
	}
	return false
}

// ValidateTerraIDClaims checks claims against an expectation, without looking
// at any signature.
//
// VerifyTerraIDToken calls it after the signature holds. The issuer calls it
// on its own output before signing, so a token Terra ID would refuse to accept
// is never minted in the first place. Every failure wraps
// ErrTerraIDTokenInvalid.
func ValidateTerraIDClaims(claims TerraIDClaims, expect TerraIDExpectation) error {
	if err := validateTerraIDExpectation(expect); err != nil {
		return err
	}
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrTerraIDTokenInvalid}, args...)...)
	}

	if claims.Issuer != expect.Issuer {
		return invalid("iss %q is not the expected issuer %q", claims.Issuer, expect.Issuer)
	}

	if expect.RequireTerraIDSubject {
		if !ValidTerraAccountID(claims.Subject) {
			return invalid("sub %q is not a Terra ID account id", claims.Subject)
		}
	} else if len(claims.Subject) == 0 || len(claims.Subject) > terraIDMaxSubjectBytes {
		return invalid("sub must be 1..%d bytes, got %d", terraIDMaxSubjectBytes, len(claims.Subject))
	}

	// Exactly one audience, and it is this Tree. A token valid for several
	// Trees at once is a token any one of them can replay against the rest.
	if len(claims.Audience) != 1 {
		return invalid("aud must name exactly one Tree, got %d values", len(claims.Audience))
	}
	if claims.Audience[0] != expect.Audience {
		return invalid("aud %q is not %q", claims.Audience[0], expect.Audience)
	}

	// Purpose before time: the lifetime cap depends on it.
	if expect.Use == TerraIDUseTreeLogin {
		if claims.Use != "" && claims.Use != TerraIDUseTreeLogin {
			return invalid("terra_use %q is not a login", claims.Use)
		}
	} else {
		if claims.Use != expect.Use {
			return invalid("terra_use %q is not the expected %q", claims.Use, expect.Use)
		}
		if claims.JWTID == "" {
			return invalid("jti is required on a %q assertion", expect.Use)
		}
	}
	if len(claims.JWTID) > terraIDMaxJTIBytes {
		return invalid("jti must be at most %d bytes, got %d", terraIDMaxJTIBytes, len(claims.JWTID))
	}

	if claims.ExpiresAt == 0 {
		return invalid("exp is required")
	}
	if claims.IssuedAt == 0 {
		return invalid("iat is required")
	}
	exp := time.Unix(claims.ExpiresAt, 0)
	iat := time.Unix(claims.IssuedAt, 0)
	if !exp.After(expect.Now.Add(-TerraIDClockLeeway)) {
		return invalid("expired at %s", exp.UTC().Format(time.RFC3339))
	}
	if iat.After(expect.Now.Add(TerraIDClockLeeway)) {
		return invalid("issued in the future at %s", iat.UTC().Format(time.RFC3339))
	}
	if !exp.After(iat) {
		return invalid("exp must be after iat")
	}
	limit, _ := terraIDMaxLifetime(expect.Use)
	if lifetime := exp.Sub(iat); lifetime > limit {
		return invalid("lifetime %s exceeds %s for %q", lifetime, limit, expect.Use)
	}
	if claims.NotBefore != 0 && time.Unix(claims.NotBefore, 0).After(expect.Now.Add(TerraIDClockLeeway)) {
		return invalid("not valid before %s", time.Unix(claims.NotBefore, 0).UTC().Format(time.RFC3339))
	}
	if claims.AuthTime != 0 && time.Unix(claims.AuthTime, 0).After(expect.Now.Add(TerraIDClockLeeway)) {
		return invalid("auth_time is in the future")
	}

	if claims.Actor != nil && claims.Actor.Subject == "" {
		return invalid("act must carry a sub")
	}

	if expect.Use == TerraIDUseExternalIdentityRevoke {
		if !knownRevokeReason(claims.Reason) {
			return invalid("terra_reason %q is not a known revocation reason", claims.Reason)
		}
	} else if claims.Reason != "" {
		return invalid("terra_reason is only allowed on %q", TerraIDUseExternalIdentityRevoke)
	}
	if claims.ParentNodeID != nil {
		if expect.Use != TerraIDUseNodeEnrollCodeIssue {
			return invalid("terra_parent_node_id is only allowed on %q", TerraIDUseNodeEnrollCodeIssue)
		}
		if parent := *claims.ParentNodeID; parent != "" && !ValidNodeID(parent) {
			return invalid("terra_parent_node_id %q is not a node id", parent)
		}
	}
	return nil
}

// validateTerraIDExpectation refuses an expectation that would accept "any".
// It is a caller bug rather than a bad token, but it still fails closed with
// the same sentinel: the token was not shown to be valid.
func validateTerraIDExpectation(expect TerraIDExpectation) error {
	if !ValidIssuerURL(expect.Issuer) {
		return fmt.Errorf("%w: expectation: issuer %q is not an issuer URL", ErrTerraIDTokenInvalid, expect.Issuer)
	}
	if _, err := ParseTreeAudience(expect.Audience); err != nil {
		return fmt.Errorf("%w: expectation: %v", ErrTerraIDTokenInvalid, err)
	}
	if _, known := terraIDMaxLifetime(expect.Use); !known {
		return fmt.Errorf("%w: expectation: use %q is not a known purpose", ErrTerraIDTokenInvalid, expect.Use)
	}
	if expect.Now.IsZero() {
		return fmt.Errorf("%w: expectation: now is required", ErrTerraIDTokenInvalid)
	}
	return nil
}
