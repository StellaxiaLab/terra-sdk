package trust

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const AttestKindTree = "tree"

const AttestKindMarket = "market"

const MarketIssuerPrefix = "market:"

const TreeIssuerPrefix = "tree:"

const PublisherIssuerPrefix = "publisher:"

const MarketRoleAttest = "attest"

const MarketRoleRevoke = "revoke"

const marketCertKind = "terra.market.keycert"

var ErrMarketDisabled = errors.New("market trust root is not configured")

var ErrMarketCertInvalid = errors.New("market key certificate is invalid")

var ErrMarketCertExpired = errors.New("market key certificate is outside its validity window")

// MarketRoot is the embedded trust root: offline public keys plus the number
// of DISTINCT keys that must sign a certificate (e.g. 2 of 3). Key.Tier is
// ignored.
type MarketRoot struct {
	MarketID  string
	Threshold int
	Keys      []TrustKey
}

// Enabled reports whether the root can verify anything. A product that embeds
// no root has the market switched off.
func (r MarketRoot) Enabled() bool {
	return r.Threshold > 0 && len(r.Keys) >= r.Threshold
}

// Issuer is the chain issuer string of this market ("market:<id>").
func (r MarketRoot) Issuer() string { return MarketIssuerPrefix + r.MarketID }

// Fingerprint is a stable hex digest of market id, threshold and the key set
// (order independent) — what a UI/log shows as "the market root".
func (r MarketRoot) Fingerprint() string {
	ids := make([]string, 0, len(r.Keys))
	for _, key := range r.Keys {
		id := key.KeyID
		if id == "" {
			id = TrustKeyID(key.PublicKey)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", r.MarketID, r.Threshold, strings.Join(ids, ","))))
	return hex.EncodeToString(sum[:])
}

// MarketKeyCertificate is a root-signed certificate for an online market key.
type MarketKeyCertificate struct {
	Role       string             `json:"role"`      // attest | revoke
	KeyID      string             `json:"keyId"`     // TrustKeyID(publicKey)
	PublicKey  string             `json:"publicKey"` // base64url raw ed25519
	Epoch      int64              `json:"epoch"`     // monotonic per role
	NotBefore  string             `json:"notBefore"` // RFC3339
	NotAfter   string             `json:"notAfter"`  // RFC3339
	Signatures []CounterSignature `json:"signatures"`
}

// CanonicalMarketCertPayload is what the root signs: everything except the
// signatures, in fixed order, kind and marketId first.
func CanonicalMarketCertPayload(marketID string, c MarketKeyCertificate) ([]byte, error) {
	return json.Marshal(struct {
		Kind      string `json:"kind"`
		MarketID  string `json:"marketId"`
		Role      string `json:"role"`
		KeyID     string `json:"keyId"`
		PublicKey string `json:"publicKey"`
		Epoch     int64  `json:"epoch"`
		NotBefore string `json:"notBefore"`
		NotAfter  string `json:"notAfter"`
	}{marketCertKind, marketID, c.Role, c.KeyID, c.PublicKey, c.Epoch, c.NotBefore, c.NotAfter})
}

// SignMarketCertificate produces one root key's signature over c.
func SignMarketCertificate(marketID string, c MarketKeyCertificate, rootPriv ed25519.PrivateKey, signedAt string) (CounterSignature, error) {
	if len(rootPriv) != ed25519.PrivateKeySize {
		return CounterSignature{}, errors.New("invalid ed25519 private key")
	}
	payload, err := CanonicalMarketCertPayload(marketID, c)
	if err != nil {
		return CounterSignature{}, err
	}
	return CounterSignature{
		Algorithm: AlgorithmEd25519,
		KeyID:     TrustKeyID(rootPriv.Public().(ed25519.PublicKey)),
		Issuer:    MarketIssuerPrefix + marketID,
		SignedAt:  signedAt,
		Value:     base64.RawURLEncoding.EncodeToString(ed25519.Sign(rootPriv, payload)),
	}, nil
}

// VerifyMarketCertificate checks the threshold of DISTINCT root signatures,
// the validity window at now, the role, and that keyId matches the key. It
// returns a TrustKey (Tier trusted-third-party, Issuer "market:<id>").
func VerifyMarketCertificate(root MarketRoot, c MarketKeyCertificate, wantRole string, now time.Time) (TrustKey, error) {
	if !root.Enabled() {
		return TrustKey{}, ErrMarketDisabled
	}
	if c.Role != wantRole || (wantRole != MarketRoleAttest && wantRole != MarketRoleRevoke) {
		return TrustKey{}, fmt.Errorf("%w: role %q, want %q", ErrMarketCertInvalid, c.Role, wantRole)
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return TrustKey{}, fmt.Errorf("%w: bad public key", ErrMarketCertInvalid)
	}
	public := ed25519.PublicKey(raw)
	if c.KeyID != TrustKeyID(public) {
		return TrustKey{}, fmt.Errorf("%w: keyId does not match the key", ErrMarketCertInvalid)
	}
	notBefore, err1 := time.Parse(time.RFC3339, c.NotBefore)
	notAfter, err2 := time.Parse(time.RFC3339, c.NotAfter)
	if err1 != nil || err2 != nil || !notAfter.After(notBefore) {
		return TrustKey{}, fmt.Errorf("%w: bad validity window", ErrMarketCertInvalid)
	}
	if now.Before(notBefore) || !now.Before(notAfter) {
		return TrustKey{}, ErrMarketCertExpired
	}
	payload, err := CanonicalMarketCertPayload(root.MarketID, c)
	if err != nil {
		return TrustKey{}, err
	}
	rootByID := make(map[string]ed25519.PublicKey, len(root.Keys))
	for _, key := range root.Keys {
		id := key.KeyID
		if id == "" {
			id = TrustKeyID(key.PublicKey)
		}
		rootByID[id] = key.PublicKey
	}
	signed := map[string]bool{}
	for _, sig := range c.Signatures {
		if !strings.EqualFold(sig.Algorithm, AlgorithmEd25519) || signed[sig.KeyID] {
			continue
		}
		key, ok := rootByID[sig.KeyID]
		if !ok || len(key) != ed25519.PublicKeySize {
			continue
		}
		value, err := base64.RawURLEncoding.DecodeString(sig.Value)
		if err != nil || len(value) != ed25519.SignatureSize {
			continue
		}
		if ed25519.Verify(key, payload, value) {
			signed[sig.KeyID] = true
		}
	}
	if len(signed) < root.Threshold {
		return TrustKey{}, fmt.Errorf("%w: %d of %d root signatures", ErrMarketCertInvalid, len(signed), root.Threshold)
	}
	return TrustKey{KeyID: c.KeyID, Issuer: root.Issuer(), Tier: TrustTrustedThirdParty, PublicKey: public}, nil
}

// ChainAnchors are the two anchor kinds a node holds. They are never mixed
// (a verifier checks them separately).
type ChainAnchors struct {
	Tree   []TrustKey  // subtree anchors received at enrollment
	Market *MarketRoot // product-embedded; nil → market not checked
}

// Attestation is one verified provenance entry of a package.
type Attestation struct {
	Kind     string `json:"kind"` // AttestKindTree | AttestKindMarket
	KeyID    string `json:"keyId"`
	Issuer   string `json:"issuer"`
	SignedAt string `json:"signedAt,omitempty"`
}

// ChainResult records EVERY verified attestation (both are recorded, not
// "the higher one").
type ChainResult struct {
	// Tier is trusted-third-party if any market attestation verified, else the
	// tree anchor's tier (tree-published by default), else "".
	Tier         TrustTier
	KeyID        string // of the attestation that decided Tier (market first)
	Issuer       string
	Attestations []Attestation
}

// Endorsed reports whether any attestation verified.
func (r ChainResult) Endorsed() bool { return r.Tier != "" }

// HasMarket reports whether a market attestation verified.
func (r ChainResult) HasMarket() bool {
	for _, a := range r.Attestations {
		if a.Kind == AttestKindMarket {
			return true
		}
	}
	return false
}

// Provenance lists the attestation issuers ("market:terra", "tree:<id>").
func (r ChainResult) Provenance() []string {
	out := make([]string, 0, len(r.Attestations))
	for _, a := range r.Attestations {
		out = append(out, a.Issuer)
	}
	return out
}

// VerifyChain verifies the whole chain. An entry whose key is unknown,
// uncertified, expired or whose signature fails is skipped (never an error);
// it simply contributes no attestation. With anchors.Market == nil (or a
// disabled root) only tree anchors are consulted, as before.
func VerifyChain(raw []byte, chain []CounterSignature, anchors ChainAnchors, now time.Time) ChainResult {
	var result ChainResult
	if len(chain) == 0 {
		return result
	}
	payload, err := CanonicalCounterSignPayload(raw)
	if err != nil {
		return result
	}
	treeByID := make(map[string]TrustKey, len(anchors.Tree))
	for _, anchor := range anchors.Tree {
		keyID := anchor.KeyID
		if keyID == "" {
			keyID = TrustKeyID(anchor.PublicKey)
		}
		treeByID[keyID] = anchor
	}
	var marketDecided, treeDecided bool
	var marketKey, marketIssuer, treeKey, treeIssuer string
	var treeTier TrustTier
	for _, entry := range chain {
		if !strings.EqualFold(entry.Algorithm, AlgorithmEd25519) {
			continue
		}
		signature, err := base64.RawURLEncoding.DecodeString(entry.Value)
		if err != nil || len(signature) != ed25519.SignatureSize {
			continue
		}
		if strings.HasPrefix(entry.Issuer, MarketIssuerPrefix) {
			root := anchors.Market
			if root == nil || !root.Enabled() || entry.Cert == nil || entry.Issuer != root.Issuer() {
				continue
			}
			key, err := VerifyMarketCertificate(*root, *entry.Cert, MarketRoleAttest, now)
			if err != nil || key.KeyID != entry.KeyID {
				continue
			}
			if !ed25519.Verify(key.PublicKey, payload, signature) {
				continue
			}
			result.Attestations = append(result.Attestations, Attestation{Kind: AttestKindMarket, KeyID: entry.KeyID, Issuer: entry.Issuer, SignedAt: entry.SignedAt})
			if !marketDecided {
				marketDecided, marketKey, marketIssuer = true, entry.KeyID, entry.Issuer
			}
			continue
		}
		anchor, known := treeByID[entry.KeyID]
		if !known || len(anchor.PublicKey) != ed25519.PublicKeySize {
			continue
		}
		if !ed25519.Verify(anchor.PublicKey, payload, signature) {
			continue
		}
		result.Attestations = append(result.Attestations, Attestation{Kind: AttestKindTree, KeyID: entry.KeyID, Issuer: entry.Issuer, SignedAt: entry.SignedAt})
		if !treeDecided {
			treeDecided, treeKey, treeIssuer = true, entry.KeyID, entry.Issuer
			treeTier = anchor.Tier
			if treeTier == "" {
				treeTier = TrustTreePublished
			}
		}
	}
	switch {
	case marketDecided:
		result.Tier, result.KeyID, result.Issuer = TrustTrustedThirdParty, marketKey, marketIssuer
	case treeDecided:
		result.Tier, result.KeyID, result.Issuer = treeTier, treeKey, treeIssuer
	}
	return result
}
