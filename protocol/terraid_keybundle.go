package protocol

// The offline key bundle: Terra ID's verification keys, delivered with the
// installer or written by an administrator, for a Tree that cannot reach the
// issuer's JWKS.
//
// Without it, a Tree whose cached JWKS has expired and whose network to the
// issuer is down refuses every Terra ID login — exactly when an operator most
// needs to get in. The bundle carries the current key and the next one, so it
// keeps working across one rotation.
//
// The bundle is NOT signed in this first version (ADR-ID-001 Q-ID-4). Its
// integrity comes from how it arrives: inside a signed installer, or written by
// someone who already administers the Tree. That is why it has an expiry and an
// issuer and nothing else that would look like a trust decision: a bundle is
// keys for one issuer the Tree already trusts, for a bounded time. Which source
// wins — a live JWKS, a cached one, or the bundle — is the Tree's policy (P2).

import (
	"fmt"
	"time"
)

// TerraIDKeyBundleVersion is the only bundle format version understood.
const TerraIDKeyBundleVersion = 1

// TerraIDKeyBundle is the offline key bundle.
type TerraIDKeyBundle struct {
	Version  int       `json:"version"`
	Issuer   string    `json:"issuer"`
	Keys     JWKSet    `json:"keys"`
	IssuedAt time.Time `json:"issued_at"`
	NotAfter time.Time `json:"not_after"`
}

// ValidateTerraIDKeyBundle refuses a bundle that must not be used at now: an
// unknown version, a malformed issuer, keys ParseJWKSet would refuse, or a
// window that is empty or already over. Failures wrap
// ErrTerraIDKeyBundleInvalid (and ErrTerraIDKeyInvalid for a key problem).
func ValidateTerraIDKeyBundle(bundle TerraIDKeyBundle, now time.Time) error {
	if bundle.Version != TerraIDKeyBundleVersion {
		return fmt.Errorf("%w: version %d is not %d", ErrTerraIDKeyBundleInvalid, bundle.Version, TerraIDKeyBundleVersion)
	}
	if !ValidIssuerURL(bundle.Issuer) {
		return fmt.Errorf("%w: issuer %q is not an issuer URL", ErrTerraIDKeyBundleInvalid, bundle.Issuer)
	}
	if err := validateJWKSet(bundle.Keys); err != nil {
		return fmt.Errorf("%w: %w", ErrTerraIDKeyBundleInvalid, err)
	}
	if bundle.IssuedAt.IsZero() || bundle.NotAfter.IsZero() {
		return fmt.Errorf("%w: issued_at and not_after are required", ErrTerraIDKeyBundleInvalid)
	}
	if !bundle.NotAfter.After(bundle.IssuedAt) {
		return fmt.Errorf("%w: not_after must be after issued_at", ErrTerraIDKeyBundleInvalid)
	}
	if !bundle.NotAfter.After(now) {
		return fmt.Errorf("%w: expired at %s", ErrTerraIDKeyBundleInvalid, bundle.NotAfter.UTC().Format(time.RFC3339))
	}
	return nil
}

// KeysFor returns the bundle's keys for verifying tokens from issuer at now.
//
// The issuer is compared byte for byte, like the token's iss: a bundle for one
// issuer must never supply keys for another, or a Tree trusting two issuers
// would accept either one's tokens under the other's name.
func (b TerraIDKeyBundle) KeysFor(issuer string, now time.Time) (JWKSet, error) {
	if err := ValidateTerraIDKeyBundle(b, now); err != nil {
		return JWKSet{}, err
	}
	if b.Issuer != issuer {
		return JWKSet{}, fmt.Errorf("%w: bundle is for %q, not %q", ErrTerraIDKeyBundleInvalid, b.Issuer, issuer)
	}
	return b.Keys, nil
}
