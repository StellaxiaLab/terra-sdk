package protocol

// Signatures on Terra ID tokens: JWS compact serialization (RFC 7515) with JWK
// public keys (RFC 7517), restricted to the two algorithms Terra ID signs with.
//
// Almost every published JWT vulnerability is a verifier being generous: taking
// `alg` from the token and believing it (`none`, or an HMAC keyed with a public
// key), trying every key until one fits, or ignoring a `crit` extension it does
// not implement. This verifier is ungenerous on each of those on purpose:
//
//   - two algorithms, named here, and the key must independently agree with the
//     header — the header never picks the algorithm on its own;
//   - the key is the one whose kid matches exactly, or nothing; there is no
//     fallback to "the other key", because a fallback is a way for a token signed
//     by a retired or foreign key to find a home;
//   - any `crit` is refused, since no extension is understood;
//   - `typ` separates logins from assertions before any claim is read.
//
// ES256 signatures are R||S, 64 bytes, as RFC 7518 §3.4 requires — not the
// ASN.1 DER that Go's crypto.Signer produces. Getting that wrong is the classic
// interoperability bug between a Go issuer and every other verifier, so the
// conversion is done here once, for both sides.

import (
	"bytes"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// JWS algorithms Terra ID tokens may use.
const (
	JWSAlgEdDSA = "EdDSA"
	JWSAlgES256 = "ES256"
)

// JWK key types and curves accepted.
const (
	JWKKeyTypeOKP   = "OKP"
	JWKKeyTypeEC    = "EC"
	JWKCurveEd25519 = "Ed25519"
	JWKCurveP256    = "P-256"
	// JWKUseSignature is the only `use` a verifying key may declare.
	JWKUseSignature = "sig"
)

// maxTerraIDTokenBytes bounds what the verifier will decode. A real token is a
// few hundred bytes; the bound only keeps a hostile one from costing more.
const maxTerraIDTokenBytes = 16 << 10

// p256CoordinateBytes is the size of one P-256 coordinate and of R or S.
const p256CoordinateBytes = 32

// b64 is unpadded base64url, strict about trailing bits so that one byte
// string has exactly one encoding.
var b64 = base64.RawURLEncoding.Strict()

// JWK is a public verification key (RFC 7517). Only OKP/Ed25519 and EC/P-256
// are understood.
type JWK struct {
	KeyType string `json:"kty"`
	Curve   string `json:"crv"`
	KeyID   string `json:"kid"`
	X       string `json:"x"`
	Y       string `json:"y,omitempty"`
	Alg     string `json:"alg,omitempty"`
	Use     string `json:"use,omitempty"`
}

// JWKSet is a JWKS document: {"keys": [...]}.
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// ParseJWKSet reads and checks a JWKS document.
//
// A key it does not understand is an error, not something to skip. Skipping
// would make a set that silently lost its only usable key look healthy, and
// the failure would surface later as "every login is refused" with no hint why.
func ParseJWKSet(raw []byte) (JWKSet, error) {
	var set JWKSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return JWKSet{}, fmt.Errorf("%w: jwks is not a JSON key set: %v", ErrTerraIDKeyInvalid, err)
	}
	if err := validateJWKSet(set); err != nil {
		return JWKSet{}, err
	}
	return set, nil
}

// validateJWKSet is ParseJWKSet's checks on an already-decoded set: at least
// one key, every key usable, and no kid twice — a duplicate kid would make
// "the key with this kid" two keys.
func validateJWKSet(set JWKSet) error {
	if len(set.Keys) == 0 {
		return fmt.Errorf("%w: key set has no keys", ErrTerraIDKeyInvalid)
	}
	seen := make(map[string]bool, len(set.Keys))
	for i, key := range set.Keys {
		if _, err := key.publicKey(); err != nil {
			return fmt.Errorf("key %d: %w", i, err)
		}
		if seen[key.KeyID] {
			return fmt.Errorf("%w: kid %q appears more than once", ErrTerraIDKeyInvalid, key.KeyID)
		}
		seen[key.KeyID] = true
	}
	return nil
}

// algorithm is the JWS alg this key can verify, from its kty and crv.
func (k JWK) algorithm() (string, error) {
	switch {
	case k.KeyType == JWKKeyTypeOKP && k.Curve == JWKCurveEd25519:
		return JWSAlgEdDSA, nil
	case k.KeyType == JWKKeyTypeEC && k.Curve == JWKCurveP256:
		return JWSAlgES256, nil
	}
	return "", fmt.Errorf("%w: kty %q crv %q is not supported", ErrTerraIDKeyInvalid, k.KeyType, k.Curve)
}

// publicKey checks the key and returns it as an ed25519.PublicKey or an
// *ecdsa.PublicKey.
func (k JWK) publicKey() (crypto.PublicKey, error) {
	if k.KeyID == "" {
		return nil, fmt.Errorf("%w: kid is required", ErrTerraIDKeyInvalid)
	}
	alg, err := k.algorithm()
	if err != nil {
		return nil, err
	}
	if k.Alg != "" && k.Alg != alg {
		return nil, fmt.Errorf("%w: kid %q: alg %q does not match kty %q crv %q",
			ErrTerraIDKeyInvalid, k.KeyID, k.Alg, k.KeyType, k.Curve)
	}
	if k.Use != "" && k.Use != JWKUseSignature {
		return nil, fmt.Errorf("%w: kid %q: use %q is not %q", ErrTerraIDKeyInvalid, k.KeyID, k.Use, JWKUseSignature)
	}
	x, err := b64.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("%w: kid %q: x is not base64url", ErrTerraIDKeyInvalid, k.KeyID)
	}

	if alg == JWSAlgEdDSA {
		if k.Y != "" {
			return nil, fmt.Errorf("%w: kid %q: an Ed25519 key has no y", ErrTerraIDKeyInvalid, k.KeyID)
		}
		if len(x) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: kid %q: x must be %d bytes, got %d",
				ErrTerraIDKeyInvalid, k.KeyID, ed25519.PublicKeySize, len(x))
		}
		return ed25519.PublicKey(x), nil
	}

	y, err := b64.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("%w: kid %q: y is not base64url", ErrTerraIDKeyInvalid, k.KeyID)
	}
	if len(x) != p256CoordinateBytes || len(y) != p256CoordinateBytes {
		return nil, fmt.Errorf("%w: kid %q: P-256 coordinates must be %d bytes each",
			ErrTerraIDKeyInvalid, k.KeyID, p256CoordinateBytes)
	}
	// crypto/ecdh refuses points off the curve and the point at infinity.
	// An off-curve "public key" is how invalid-curve attacks start, so it is
	// checked when the key is read, not trusted to the signature check.
	point := append(append([]byte{4}, x...), y...)
	if _, err := ecdh.P256().NewPublicKey(point); err != nil {
		return nil, fmt.Errorf("%w: kid %q: point is not on P-256", ErrTerraIDKeyInvalid, k.KeyID)
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
}

// JWKFromPublicKey describes a public key as a JWK with alg and use set. It
// takes an ed25519.PublicKey or a P-256 *ecdsa.PublicKey.
func JWKFromPublicKey(kid string, pub crypto.PublicKey) (JWK, error) {
	if kid == "" {
		return JWK{}, fmt.Errorf("%w: kid is required", ErrTerraIDKeyInvalid)
	}
	switch key := pub.(type) {
	case ed25519.PublicKey:
		if len(key) != ed25519.PublicKeySize {
			return JWK{}, fmt.Errorf("%w: Ed25519 public key must be %d bytes", ErrTerraIDKeyInvalid, ed25519.PublicKeySize)
		}
		return JWK{KeyType: JWKKeyTypeOKP, Curve: JWKCurveEd25519, KeyID: kid,
			X: b64.EncodeToString(key), Alg: JWSAlgEdDSA, Use: JWKUseSignature}, nil
	case *ecdsa.PublicKey:
		if key == nil || key.Curve != elliptic.P256() {
			return JWK{}, fmt.Errorf("%w: only P-256 ECDSA keys are supported", ErrTerraIDKeyInvalid)
		}
		ecdhKey, err := key.ECDH()
		if err != nil {
			return JWK{}, fmt.Errorf("%w: %v", ErrTerraIDKeyInvalid, err)
		}
		point := ecdhKey.Bytes() // 0x04 || X || Y
		return JWK{KeyType: JWKKeyTypeEC, Curve: JWKCurveP256, KeyID: kid,
			X:   b64.EncodeToString(point[1 : 1+p256CoordinateBytes]),
			Y:   b64.EncodeToString(point[1+p256CoordinateBytes:]),
			Alg: JWSAlgES256, Use: JWKUseSignature}, nil
	}
	return JWK{}, fmt.Errorf("%w: unsupported public key type %T", ErrTerraIDKeyInvalid, pub)
}

// jwsHeader is the protected header as written. Field order is fixed so that
// signing the same claims with the same Ed25519 key gives the same string.
type jwsHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ,omitempty"`
}

// VerifyTerraIDToken checks a compact JWS end to end: header, key, signature,
// then claims (ValidateTerraIDClaims). Every failure wraps
// ErrTerraIDTokenInvalid with the reason.
func VerifyTerraIDToken(compact string, keys JWKSet, expect TerraIDExpectation) (TerraIDClaims, error) {
	invalid := func(format string, args ...any) (TerraIDClaims, error) {
		return TerraIDClaims{}, fmt.Errorf("%w: "+format, append([]any{ErrTerraIDTokenInvalid}, args...)...)
	}
	if err := validateTerraIDExpectation(expect); err != nil {
		return TerraIDClaims{}, err
	}
	if len(compact) > maxTerraIDTokenBytes {
		return invalid("token is %d bytes, over %d", len(compact), maxTerraIDTokenBytes)
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return invalid("not a compact JWS (%d parts)", len(parts))
	}

	rawHeader, err := b64.DecodeString(parts[0])
	if err != nil {
		return invalid("header is not base64url")
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(rawHeader, &members); err != nil {
		return invalid("header is not a JSON object")
	}
	if _, present := members["crit"]; present {
		return invalid("crit is not supported")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(rawHeader, &header); err != nil {
		return invalid("header members have the wrong types")
	}

	switch header.Alg {
	case JWSAlgEdDSA, JWSAlgES256:
	default:
		return invalid("alg %q is not allowed", header.Alg)
	}
	if err := checkTerraIDType(header.Typ, expect.Use); err != nil {
		return TerraIDClaims{}, err
	}
	if header.Kid == "" {
		return invalid("kid is required")
	}

	// Exactly the key with this kid. No other key is tried.
	var matched []JWK
	for _, key := range keys.Keys {
		if key.KeyID == header.Kid {
			matched = append(matched, key)
		}
	}
	if len(matched) != 1 {
		return invalid("kid %q matches %d keys", header.Kid, len(matched))
	}
	key := matched[0]
	public, err := key.publicKey()
	if err != nil {
		return invalid("%v", err)
	}
	keyAlg, _ := key.algorithm()
	if keyAlg != header.Alg {
		return invalid("alg %q does not match key %q (%s %s)", header.Alg, key.KeyID, key.KeyType, key.Curve)
	}

	signature, err := b64.DecodeString(parts[2])
	if err != nil {
		return invalid("signature is not base64url")
	}
	signingInput := []byte(parts[0] + "." + parts[1])
	switch pub := public.(type) {
	case ed25519.PublicKey:
		if len(signature) != ed25519.SignatureSize || !ed25519.Verify(pub, signingInput, signature) {
			return invalid("signature does not verify")
		}
	case *ecdsa.PublicKey:
		if len(signature) != 2*p256CoordinateBytes {
			return invalid("ES256 signature must be %d bytes (R||S), got %d", 2*p256CoordinateBytes, len(signature))
		}
		digest := sha256.Sum256(signingInput)
		r := new(big.Int).SetBytes(signature[:p256CoordinateBytes])
		s := new(big.Int).SetBytes(signature[p256CoordinateBytes:])
		if !ecdsa.Verify(pub, digest[:], r, s) {
			return invalid("signature does not verify")
		}
	}

	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return invalid("payload is not base64url")
	}
	var claims TerraIDClaims
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&claims); err != nil {
		return invalid("payload is not a claim set: %v", err)
	}
	if decoder.More() {
		return invalid("payload has trailing data")
	}
	if err := ValidateTerraIDClaims(claims, expect); err != nil {
		return TerraIDClaims{}, err
	}
	return claims, nil
}

// checkTerraIDType applies §2.1: an assertion must say it is one, and a login
// must not. Media type names compare case-insensitively (RFC 7515 §4.1.9).
func checkTerraIDType(typ, use string) error {
	if use == TerraIDUseTreeLogin {
		if typ == "" || strings.EqualFold(typ, TerraIDTokenType) {
			return nil
		}
		return fmt.Errorf("%w: typ %q is not a login token", ErrTerraIDTokenInvalid, typ)
	}
	if strings.EqualFold(typ, TerraIDAssertionType) {
		return nil
	}
	return fmt.Errorf("%w: typ %q is not %q", ErrTerraIDTokenInvalid, typ, TerraIDAssertionType)
}

// SignTerraIDToken signs claims as a compact JWS.
//
// The key is an ed25519 or P-256 ECDSA crypto.Signer — ed25519.PrivateKey,
// *ecdsa.PrivateKey, or a KMS-backed signer exposing either public key. typ is
// "" or TerraIDTokenType for a login, TerraIDAssertionType for an assertion.
// It does not validate the claims: an issuer calls ValidateTerraIDClaims with
// the expectation it is minting for, which this function cannot know.
func SignTerraIDToken(claims TerraIDClaims, kid string, typ string, key crypto.Signer) (string, error) {
	if kid == "" {
		return "", fmt.Errorf("%w: kid is required", ErrTerraIDKeyInvalid)
	}
	switch typ {
	case "", TerraIDTokenType, TerraIDAssertionType:
	default:
		return "", fmt.Errorf("%w: typ %q is not a Terra ID token type", ErrTerraIDTokenInvalid, typ)
	}
	if key == nil {
		return "", fmt.Errorf("%w: signing key is required", ErrTerraIDKeyInvalid)
	}

	var alg string
	switch pub := key.Public().(type) {
	case ed25519.PublicKey:
		alg = JWSAlgEdDSA
	case *ecdsa.PublicKey:
		if pub.Curve != elliptic.P256() {
			return "", fmt.Errorf("%w: only P-256 ECDSA keys are supported", ErrTerraIDKeyInvalid)
		}
		alg = JWSAlgES256
	default:
		return "", fmt.Errorf("%w: unsupported signing key type %T", ErrTerraIDKeyInvalid, key.Public())
	}

	header, err := json.Marshal(jwsHeader{Alg: alg, Kid: kid, Typ: typ})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("%w: claims: %v", ErrTerraIDTokenInvalid, err)
	}
	signingInput := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)

	var signature []byte
	if alg == JWSAlgEdDSA {
		signature, err = key.Sign(rand.Reader, []byte(signingInput), crypto.Hash(0))
		if err != nil {
			return "", fmt.Errorf("%w: sign: %v", ErrTerraIDKeyInvalid, err)
		}
	} else {
		digest := sha256.Sum256([]byte(signingInput))
		der, err := key.Sign(rand.Reader, digest[:], crypto.SHA256)
		if err != nil {
			return "", fmt.Errorf("%w: sign: %v", ErrTerraIDKeyInvalid, err)
		}
		signature, err = ecdsaDERToRaw(der)
		if err != nil {
			return "", err
		}
	}
	return signingInput + "." + b64.EncodeToString(signature), nil
}

// ecdsaDERToRaw turns the ASN.1 SEQUENCE{r, s} a Go signer returns into the
// fixed 64-byte R||S that JWS requires, left-padding each half.
func ecdsaDERToRaw(der []byte) ([]byte, error) {
	var parsed struct{ R, S *big.Int }
	rest, err := asn1.Unmarshal(der, &parsed)
	if err != nil || len(rest) != 0 || parsed.R == nil || parsed.S == nil ||
		parsed.R.Sign() <= 0 || parsed.S.Sign() <= 0 ||
		parsed.R.BitLen() > 8*p256CoordinateBytes || parsed.S.BitLen() > 8*p256CoordinateBytes {
		return nil, fmt.Errorf("%w: signer returned a malformed ECDSA signature", ErrTerraIDKeyInvalid)
	}
	raw := make([]byte, 2*p256CoordinateBytes)
	parsed.R.FillBytes(raw[:p256CoordinateBytes])
	parsed.S.FillBytes(raw[p256CoordinateBytes:])
	return raw, nil
}
