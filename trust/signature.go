package trust

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// TrustTier is a module trust level . A module is graded by verifying its manifest
// signature against embedded, offline trust keys. Only signed Built-in (and
// Core) and, in dev mode, unsigned Development are granted by the keyring;
// everything that fails is Quarantined and is not allowed to run.
type TrustTier string

const TrustCore TrustTier = "core"

const TrustBuiltin TrustTier = "built-in"

const TrustTrustedThirdParty TrustTier = "trusted-third-party"

const TrustDevelopment TrustTier = "development"

const TrustQuarantined TrustTier = "quarantined"

// AlgorithmEd25519 is the only signature algorithm accepted.
// It verifies offline with a public key and no external dependency, matching the
// RouteTicket signing primitive in terra-protocol.
const AlgorithmEd25519 = "ed25519"

// ModuleSignature is the manifest `signature` block for the Terra Built-in scope. Value is the base64url (raw, no padding) Ed25519 signature over the
// canonical manifest payload (the manifest with its own `signature` removed).
type ModuleSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Issuer    string `json:"issuer,omitempty"`
	Value     string `json:"value"`
}

// TrustKey is an embedded, offline-verifiable public key. A signing key may only
// grant Core, Built-in or Trusted third-party — never Development or
// Quarantined, which are policy outcomes rather than key grants.
type TrustKey struct {
	KeyID     string
	Issuer    string
	Tier      TrustTier
	PublicKey ed25519.PublicKey
}

// TrustKeyID derives a non-secret keyId from a public key: the base64url (raw)
// encoding of the first 12 bytes of its SHA-256 digest. It matches a manifest
// signature to an embedded key offline (mirrors terra-protocol RouteTicketKeyID).
func TrustKeyID(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return base64.RawURLEncoding.EncodeToString(digest[:12])
}

// CanonicalManifestPayload returns the exact bytes a signature covers: the
// manifest with any `signature` block removed, re-serialized as canonical JSON
// (recursively key-sorted, format-normalized). Signer and verifier both use it,
// so re-formatting the on-disk manifest never breaks verification.
func CanonicalManifestPayload(raw []byte) ([]byte, error) {
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("manifest JSON을 해석할 수 없습니다: %w", err)
	}
	delete(generic, "signature")
	return json.Marshal(generic)
}

// SignManifest signs a manifest with an Ed25519 private key and returns the
// signature block to embed. Any existing `signature` in raw is ignored (the
// canonical payload excludes it). It is the counterpart of Verify for the Terra
// release signer and for tests.
func SignManifest(raw []byte, privateKey ed25519.PrivateKey, issuer string) (ModuleSignature, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return ModuleSignature{}, fmt.Errorf("유효한 ed25519 개인키가 아닙니다")
	}
	payload, err := CanonicalManifestPayload(raw)
	if err != nil {
		return ModuleSignature{}, err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return ModuleSignature{
		Algorithm: AlgorithmEd25519,
		KeyID:     TrustKeyID(publicKey),
		Issuer:    issuer,
		Value:     base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
	}, nil
}

// EmbedSignature injects sig into a manifest document and returns the signed
// bytes (indented JSON). The non-signature content is preserved, so a verifier
// recomputes the identical canonical payload.
func EmbedSignature(raw []byte, sig ModuleSignature) ([]byte, error) {
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("manifest JSON을 해석할 수 없습니다: %w", err)
	}
	encoded, err := json.Marshal(sig)
	if err != nil {
		return nil, err
	}
	var sigMap map[string]any
	if err := json.Unmarshal(encoded, &sigMap); err != nil {
		return nil, err
	}
	generic["signature"] = sigMap
	return json.MarshalIndent(generic, "", "  ")
}

// TrustTreePublished is the subtree-scoped tier: the package carries a
// counter-signature verifiable against an anchor this node received at
// enrollment. It is deliberately NOT grantable through the embedded keyring —
// the same package under a different tree has no matching anchor and stays
// ungraded. Global trust and subtree trust never mix (a verifier never mixes the two anchor kinds).
const TrustTreePublished TrustTier = "tree-published"

// CounterSignature is one hop's endorsement (a counter-signature). It rides OUT
// of band — in the publication record and the desired response, never inside
// the archive — so adding one changes no byte of the package: the digest and
// the author signature survive every promotion hop (promotion is not a copy).
type CounterSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Issuer    string `json:"issuer"`
	SignedAt  string `json:"signedAt,omitempty"`
	Value     string `json:"value"`
	// Cert is the root-signed attestation-key certificate; present only on a
	// market attestation entry (see Attestation). Not part of any signed payload.
	Cert *MarketKeyCertificate `json:"cert,omitempty"`
}

// CanonicalCounterSignPayload is the exact bytes a counter-signature covers:
// the canonical manifest INCLUDING the author's signature block (the endorser
// vouches for the signed artifact, author identity and all) but excluding any
// chain — the chain lives outside the covered content so hops can accrete.
func CanonicalCounterSignPayload(raw []byte) ([]byte, error) {
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("manifest JSON을 해석할 수 없습니다: %w", err)
	}
	if signature, ok := generic["signature"].(map[string]any); ok {
		delete(signature, "chain")
	}
	return json.Marshal(generic)
}

// CounterSignManifest produces one hop's counter-signature over raw with an
// Ed25519 key (a tree's service identity). signedAt is the caller's clock in
// RFC3339; empty is allowed (tests).
func CounterSignManifest(raw []byte, privateKey ed25519.PrivateKey, issuer, signedAt string) (CounterSignature, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return CounterSignature{}, fmt.Errorf("유효한 ed25519 개인키가 아닙니다")
	}
	payload, err := CanonicalCounterSignPayload(raw)
	if err != nil {
		return CounterSignature{}, err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return CounterSignature{
		Algorithm: AlgorithmEd25519,
		KeyID:     TrustKeyID(publicKey),
		Issuer:    issuer,
		SignedAt:  signedAt,
		Value:     base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
	}, nil
}
