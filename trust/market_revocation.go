package trust

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RevocationReason says what a revocation means for what is already installed.
type RevocationReason string

const RevokeSecurity RevocationReason = "security"

const RevokePolicy RevocationReason = "policy"

const RevokePublisher RevocationReason = "publisher"

// RevokedEntry revokes id@version ("*" = every version); a non-empty Digest
// narrows it to that package digest only.
type RevokedEntry struct {
	ID      string           `json:"id"`
	Version string           `json:"version"`
	Digest  string           `json:"digest,omitempty"`
	Reason  RevocationReason `json:"reason"`
}

// RevocationSnapshot is the market's signed statement of what is revoked and
// what the newest version of each module is.
type RevocationSnapshot struct {
	MarketID  string               `json:"marketId"`
	Sequence  int64                `json:"sequence"`
	IssuedAt  string               `json:"issuedAt"`
	ExpiresAt string               `json:"expiresAt"`
	Revoked   []RevokedEntry       `json:"revoked"`
	Latest    map[string]string    `json:"latest"`
	Cert      MarketKeyCertificate `json:"cert"`
	Signature CounterSignature     `json:"signature"`
}

var ErrSnapshotRollback = errors.New("revocation snapshot sequence is lower than the one already accepted")

var ErrSnapshotSame = errors.New("revocation snapshot sequence already accepted")

var ErrSnapshotSignature = errors.New("revocation snapshot signature is invalid")

var ErrSnapshotExpiredNew = errors.New("revocation snapshot is already expired")

var ErrRevocationStale = errors.New("no fresh revocation snapshot")

var ErrRevoked = errors.New("package is revoked")

var ErrDowngrade = errors.New("install would downgrade without a pin")

// CanonicalSnapshotPayload is what the revoke key signs: the snapshot minus
// Cert and Signature, Revoked sorted by (id, version, digest), map keys
// sorted by encoding/json, kind first.
func CanonicalSnapshotPayload(s RevocationSnapshot) ([]byte, error) {
	revoked := append([]RevokedEntry{}, s.Revoked...)
	sort.SliceStable(revoked, func(i, j int) bool {
		a, b := revoked[i], revoked[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Digest < b.Digest
	})
	latest := s.Latest
	if latest == nil {
		latest = map[string]string{}
	}
	return json.Marshal(struct {
		Kind      string            `json:"kind"`
		MarketID  string            `json:"marketId"`
		Sequence  int64             `json:"sequence"`
		IssuedAt  string            `json:"issuedAt"`
		ExpiresAt string            `json:"expiresAt"`
		Revoked   []RevokedEntry    `json:"revoked"`
		Latest    map[string]string `json:"latest"`
	}{"terra.market.revocations", s.MarketID, s.Sequence, s.IssuedAt, s.ExpiresAt, revoked, latest})
}

// SignSnapshot signs s with the revoke key and attaches its certificate.
func SignSnapshot(s RevocationSnapshot, revokePriv ed25519.PrivateKey, cert MarketKeyCertificate) (RevocationSnapshot, error) {
	if len(revokePriv) != ed25519.PrivateKeySize {
		return RevocationSnapshot{}, errors.New("invalid ed25519 private key")
	}
	payload, err := CanonicalSnapshotPayload(s)
	if err != nil {
		return RevocationSnapshot{}, err
	}
	public := revokePriv.Public().(ed25519.PublicKey)
	s.Cert = cert
	s.Signature = CounterSignature{
		Algorithm: AlgorithmEd25519,
		KeyID:     TrustKeyID(public),
		Issuer:    MarketIssuerPrefix + s.MarketID,
		SignedAt:  s.IssuedAt,
		Value:     base64.RawURLEncoding.EncodeToString(ed25519.Sign(revokePriv, payload)),
	}
	return s, nil
}

// VerifySnapshot checks market id, the revoke certificate (root threshold,
// window) and the signature. Expiry is NOT an error here (see Fresh).
func VerifySnapshot(root MarketRoot, s RevocationSnapshot, now time.Time) error {
	if !root.Enabled() {
		return ErrMarketDisabled
	}
	if s.MarketID != root.MarketID {
		return fmt.Errorf("%w: market id %q", ErrSnapshotSignature, s.MarketID)
	}
	key, err := VerifyMarketCertificate(root, s.Cert, MarketRoleRevoke, now)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSnapshotSignature, err)
	}
	if s.Signature.KeyID != key.KeyID {
		return fmt.Errorf("%w: signed by a different key", ErrSnapshotSignature)
	}
	value, err := base64.RawURLEncoding.DecodeString(s.Signature.Value)
	if err != nil || len(value) != ed25519.SignatureSize {
		return ErrSnapshotSignature
	}
	payload, err := CanonicalSnapshotPayload(s)
	if err != nil {
		return err
	}
	if !ed25519.Verify(key.PublicKey, payload, value) {
		return ErrSnapshotSignature
	}
	return nil
}

// Fresh reports whether the snapshot has not expired at now. An unparsable
// ExpiresAt is not fresh.
func (s RevocationSnapshot) Fresh(now time.Time) bool {
	expires, err := time.Parse(time.RFC3339, s.ExpiresAt)
	return err == nil && now.Before(expires)
}

// RevocationVerdict is the answer to "is this package revoked".
type RevocationVerdict struct {
	Revoked bool
	Reason  RevocationReason
	Entry   RevokedEntry
}

// NormalizeDigest reduces a package digest to bare lowercase hex, so
// "sha256:<hex>" and "<hex>" (and either case) name the same package. The
// market stores a revocation's digest as the operator typed it, the node
// gates installs with "sha256:<hex>" and keeps installed digests as bare
// hex — every comparison goes through here.
func NormalizeDigest(digest string) string {
	digest = strings.ToLower(strings.TrimSpace(digest))
	return strings.TrimPrefix(digest, "sha256:")
}

// Judge matches id/version/digest against the revoked list. When several
// entries match, a security revocation wins. Digests are compared after
// NormalizeDigest.
func (s RevocationSnapshot) Judge(id, version, digest string) RevocationVerdict {
	var verdict RevocationVerdict
	digest = NormalizeDigest(digest)
	for _, entry := range s.Revoked {
		if entry.ID != id || (entry.Version != "*" && entry.Version != version) {
			continue
		}
		if want := NormalizeDigest(entry.Digest); want != "" && want != digest {
			continue
		}
		if !verdict.Revoked || (entry.Reason == RevokeSecurity && verdict.Reason != RevokeSecurity) {
			verdict = RevocationVerdict{Revoked: true, Reason: entry.Reason, Entry: entry}
		}
	}
	return verdict
}

// AcceptSnapshot verifies next and enforces monotonic sequence against prev
// (the last accepted one, nil if none). Lower sequence → ErrSnapshotRollback;
// equal → ErrSnapshotSame (harmless no-op); an already-expired next →
// ErrSnapshotExpiredNew.
func AcceptSnapshot(root MarketRoot, prev *RevocationSnapshot, next RevocationSnapshot, now time.Time) error {
	if err := VerifySnapshot(root, next, now); err != nil {
		return err
	}
	if prev != nil {
		if next.Sequence < prev.Sequence {
			return ErrSnapshotRollback
		}
		if next.Sequence == prev.Sequence {
			return ErrSnapshotSame
		}
	}
	if !next.Fresh(now) {
		return ErrSnapshotExpiredNew
	}
	return nil
}

// CheckDowngrade refuses installing a version lower than the one currently
// selected unless the operator pinned it. Pure: the caller supplies
// installedSelected ("" when not installed).
func (s RevocationSnapshot) CheckDowngrade(id, candidate, installedSelected string, pinned bool) error {
	if pinned || installedSelected == "" {
		return nil
	}
	have, okHave := parseSemver(installedSelected)
	want, okWant := parseSemver(candidate)
	if !okHave || !okWant {
		return fmt.Errorf("%w: unparsable version for %s", ErrDowngrade, id)
	}
	if compareSemver(want, have) < 0 {
		return fmt.Errorf("%w: %s %s < installed %s", ErrDowngrade, id, candidate, installedSelected)
	}
	return nil
}

// MarketGate is applied by InstallPackage when the grade came from a market
// attestation.
type MarketGate struct {
	Snapshot          *RevocationSnapshot // nil = no snapshot known
	Now               time.Time           // zero → time.Now
	Pinned            bool
	InstalledSelected string
	// AllowStale turns off the freshness requirement: a missing or expired
	// snapshot no longer refuses, while a revocation the snapshot it does
	// have names still does. It is for the tree (distribution) path, whose
	// approver is the tree, not the market — an air-gapped tree must keep
	// deploying after the snapshot TTL (the tree path is judged by its own approval, not by the snapshot clock). The direct
	// market path never sets it: there a missing gate or a stale snapshot is
	// fail-closed.
	AllowStale bool
}

// Check judges a NEW install/update: stale or missing snapshot → refuse
// (unless AllowStale); any revocation → refuse; downgrade without pin →
// refuse.
func (g MarketGate) Check(id, version, digest string) error {
	now := g.Now
	if now.IsZero() {
		now = time.Now()
	}
	if g.Snapshot == nil {
		if g.AllowStale {
			return nil
		}
		return ErrRevocationStale
	}
	if !g.AllowStale && !g.Snapshot.Fresh(now) {
		return ErrRevocationStale
	}
	if verdict := g.Snapshot.Judge(id, version, digest); verdict.Revoked {
		return fmt.Errorf("%w: %s %s (%s)", ErrRevoked, id, version, verdict.Reason)
	}
	return g.Snapshot.CheckDowngrade(id, version, g.InstalledSelected, g.Pinned)
}
