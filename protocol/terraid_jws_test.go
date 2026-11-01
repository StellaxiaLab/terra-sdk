package protocol

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
)

type terraIDTestKeys struct {
	ed     ed25519.PrivateKey
	ec     *ecdsa.PrivateKey
	edJWK  JWK
	ecJWK  JWK
	jwkSet JWKSet
}

func newTerraIDTestKeys(t *testing.T) terraIDTestKeys {
	t.Helper()
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edJWK, err := JWKFromPublicKey("ed-1", edKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	ecJWK, err := JWKFromPublicKey("ec-1", ecKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	return terraIDTestKeys{ed: edKey, ec: ecKey, edJWK: edJWK, ecJWK: ecJWK, jwkSet: JWKSet{Keys: []JWK{edJWK, ecJWK}}}
}

// craftTerraIDToken builds a compact JWS with an arbitrary header, signed by
// key under whatever alg the key implies — the header is not consulted. It is
// how the tests produce tokens SignTerraIDToken refuses to.
func craftTerraIDToken(t *testing.T, header map[string]any, claims any, key crypto.Signer) string {
	t.Helper()
	rawHeader, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	rawClaims, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := b64.EncodeToString(rawHeader) + "." + b64.EncodeToString(rawClaims)
	var signature []byte
	switch k := key.(type) {
	case ed25519.PrivateKey:
		signature = ed25519.Sign(k, []byte(input))
	case *ecdsa.PrivateKey:
		digest := sha256.Sum256([]byte(input))
		r, s, err := ecdsa.Sign(rand.Reader, k, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		signature = make([]byte, 64)
		r.FillBytes(signature[:32])
		s.FillBytes(signature[32:])
	default:
		t.Fatalf("unsupported key %T", key)
	}
	return input + "." + b64.EncodeToString(signature)
}

func TestTerraIDTokenRoundTrip(t *testing.T) {
	keys := newTerraIDTestKeys(t)
	cases := []struct {
		name string
		kid  string
		key  crypto.Signer
		use  string
		typ  string
	}{
		{"Ed25519 login", "ed-1", keys.ed, TerraIDUseTreeLogin, TerraIDTokenType},
		{"Ed25519 login no typ", "ed-1", keys.ed, TerraIDUseTreeLogin, ""},
		{"ES256 login", "ec-1", keys.ec, TerraIDUseTreeLogin, TerraIDTokenType},
		{"Ed25519 enroll assertion", "ed-1", keys.ed, TerraIDUseNodeEnrollCodeIssue, TerraIDAssertionType},
		{"ES256 enroll assertion", "ec-1", keys.ec, TerraIDUseNodeEnrollCodeIssue, TerraIDAssertionType},
		{"ES256 revoke assertion", "ec-1", keys.ec, TerraIDUseExternalIdentityRevoke, TerraIDAssertionType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := testTerraIDClaims(t, tc.use)
			token, err := SignTerraIDToken(claims, tc.kid, tc.typ, tc.key)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			got, err := VerifyTerraIDToken(token, keys.jwkSet, testTerraIDExpectation(t, tc.use))
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			if !reflect.DeepEqual(got, claims) {
				t.Fatalf("claims changed in transit:\n got %+v\nwant %+v", got, claims)
			}
		})
	}
}

func TestTerraIDES256SignatureIsRawRS(t *testing.T) {
	keys := newTerraIDTestKeys(t)
	token, err := SignTerraIDToken(testTerraIDClaims(t, TerraIDUseTreeLogin), "ec-1", "", keys.ec)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	signature, err := b64.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if len(signature) != 64 {
		t.Fatalf("ES256 signature is %d bytes, want 64 (R||S)", len(signature))
	}

	// The same signature in ASN.1 DER is refused, not reinterpreted.
	der, err := asn1.Marshal(struct{ R, S *big.Int }{
		new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	derToken := parts[0] + "." + parts[1] + "." + b64.EncodeToString(der)
	if _, err := VerifyTerraIDToken(derToken, keys.jwkSet, testTerraIDExpectation(t, TerraIDUseTreeLogin)); !errors.Is(err, ErrTerraIDTokenInvalid) {
		t.Fatalf("DER signature: %v", err)
	}
}

func TestTerraIDTokenRejected(t *testing.T) {
	keys := newTerraIDTestKeys(t)
	login := testTerraIDClaims(t, TerraIDUseTreeLogin)
	assertion := testTerraIDClaims(t, TerraIDUseNodeEnrollCodeIssue)
	loginExpect := testTerraIDExpectation(t, TerraIDUseTreeLogin)
	assertionExpect := testTerraIDExpectation(t, TerraIDUseNodeEnrollCodeIssue)

	signed := func(claims TerraIDClaims, kid, typ string, key crypto.Signer) string {
		token, err := SignTerraIDToken(claims, kid, typ, key)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	good := signed(login, "ed-1", TerraIDTokenType, keys.ed)
	goodParts := strings.Split(good, ".")

	hs256 := func() string {
		header, _ := json.Marshal(map[string]any{"alg": "HS256", "kid": "ed-1", "typ": "JWT"})
		payload, _ := json.Marshal(login)
		input := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
		// The classic confusion: HMAC keyed with the public key bytes.
		mac := hmac.New(sha256.New, []byte(keys.edJWK.X))
		mac.Write([]byte(input))
		return input + "." + b64.EncodeToString(mac.Sum(nil))
	}()
	none := func() string {
		header, _ := json.Marshal(map[string]any{"alg": "none", "kid": "ed-1"})
		payload, _ := json.Marshal(login)
		return b64.EncodeToString(header) + "." + b64.EncodeToString(payload) + "."
	}()

	// A second Ed25519 key published under ed-1 in a different set: a token
	// signed by it must not find a home via fallback in the real set.
	_, otherEd, _ := ed25519.GenerateKey(rand.Reader)

	tamperedPayload := func() string {
		changed := login
		changed.Subject = "acc_" + strings.Repeat("Z", 26)
		payload, _ := json.Marshal(changed)
		return goodParts[0] + "." + b64.EncodeToString(payload) + "." + goodParts[2]
	}()
	tamperedSignature := func() string {
		signature, _ := b64.DecodeString(goodParts[2])
		signature[0] ^= 0x01
		return goodParts[0] + "." + goodParts[1] + "." + b64.EncodeToString(signature)
	}()

	ecKeyAsEdDSA := keys.ecJWK
	ecKeyAsEdDSA.KeyID = "ec-claims-eddsa"
	ecKeyAsEdDSA.Alg = JWSAlgEdDSA // itself invalid: alg disagrees with kty/crv
	encSet := JWKSet{Keys: []JWK{func() JWK { k := keys.edJWK; k.Use = "enc"; return k }()}}

	cases := []struct {
		name   string
		token  string
		keys   JWKSet
		expect TerraIDExpectation
	}{
		{"alg none", none, keys.jwkSet, loginExpect},
		{"alg HS256", hs256, keys.jwkSet, loginExpect},
		{"alg RS256", craftTerraIDToken(t, map[string]any{"alg": "RS256", "kid": "ed-1"}, login, keys.ed), keys.jwkSet, loginExpect},
		{"alg missing", craftTerraIDToken(t, map[string]any{"kid": "ed-1"}, login, keys.ed), keys.jwkSet, loginExpect},
		{"alg lowercase", craftTerraIDToken(t, map[string]any{"alg": "eddsa", "kid": "ed-1"}, login, keys.ed), keys.jwkSet, loginExpect},
		{"kid missing", craftTerraIDToken(t, map[string]any{"alg": "EdDSA"}, login, keys.ed), keys.jwkSet, loginExpect},
		{"kid unknown", signed(login, "ed-2", "", keys.ed), keys.jwkSet, loginExpect},
		{"kid case differs", signed(login, "ED-1", "", keys.ed), keys.jwkSet, loginExpect},
		{"no fallback to another key", signed(login, "ed-1", "", otherEd), keys.jwkSet, loginExpect},
		{"crit present", craftTerraIDToken(t, map[string]any{"alg": "EdDSA", "kid": "ed-1", "crit": []string{"exp"}}, login, keys.ed), keys.jwkSet, loginExpect},
		{"crit empty", craftTerraIDToken(t, map[string]any{"alg": "EdDSA", "kid": "ed-1", "crit": []string{}}, login, keys.ed), keys.jwkSet, loginExpect},
		{"header ES256 on Ed25519 key", craftTerraIDToken(t, map[string]any{"alg": "ES256", "kid": "ed-1"}, login, keys.ed), keys.jwkSet, loginExpect},
		{"header EdDSA on P-256 key", craftTerraIDToken(t, map[string]any{"alg": "EdDSA", "kid": "ec-1"}, login, keys.ec), keys.jwkSet, loginExpect},
		{"key alg disagrees", craftTerraIDToken(t, map[string]any{"alg": "EdDSA", "kid": "ec-claims-eddsa"}, login, keys.ed),
			JWKSet{Keys: []JWK{ecKeyAsEdDSA}}, loginExpect},
		{"key use enc", good, encSet, loginExpect},
		{"duplicate kid in set", good, JWKSet{Keys: []JWK{keys.edJWK, keys.edJWK}}, loginExpect},
		{"empty key set", good, JWKSet{}, loginExpect},
		{"tampered payload", tamperedPayload, keys.jwkSet, loginExpect},
		{"tampered signature", tamperedSignature, keys.jwkSet, loginExpect},
		{"truncated signature", goodParts[0] + "." + goodParts[1] + "." + goodParts[2][:20], keys.jwkSet, loginExpect},
		{"two parts", goodParts[0] + "." + goodParts[1], keys.jwkSet, loginExpect},
		{"four parts", good + ".x", keys.jwkSet, loginExpect},
		{"padded base64", goodParts[0] + "=." + goodParts[1] + "." + goodParts[2], keys.jwkSet, loginExpect},
		{"header not JSON", b64.EncodeToString([]byte("nope")) + "." + goodParts[1] + "." + goodParts[2], keys.jwkSet, loginExpect},
		{"oversized", strings.Repeat("a", maxTerraIDTokenBytes+1), keys.jwkSet, loginExpect},
		// typ confusion, both directions.
		{"assertion typ at login", signed(login, "ed-1", TerraIDAssertionType, keys.ed), keys.jwkSet, loginExpect},
		{"other typ at login", craftTerraIDToken(t, map[string]any{"alg": "EdDSA", "kid": "ed-1", "typ": "at+jwt"}, login, keys.ed), keys.jwkSet, loginExpect},
		{"login typ at assertion", signed(assertion, "ed-1", TerraIDTokenType, keys.ed), keys.jwkSet, assertionExpect},
		{"no typ at assertion", signed(assertion, "ed-1", "", keys.ed), keys.jwkSet, assertionExpect},
		{"login token at assertion", signed(login, "ed-1", TerraIDAssertionType, keys.ed), keys.jwkSet, assertionExpect},
		// Signature holds, claims do not.
		{"valid signature, wrong audience", signed(func() TerraIDClaims {
			c := login
			c.Audience = TerraIDAudience{"tree:node_1787923476493_0000000000000000"}
			return c
		}(), "ed-1", "", keys.ed), keys.jwkSet, loginExpect},
		{"payload not an object", craftTerraIDToken(t, map[string]any{"alg": "EdDSA", "kid": "ed-1"}, []string{"x"}, keys.ed), keys.jwkSet, loginExpect},
		{"bad expectation", good, keys.jwkSet, TerraIDExpectation{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims, err := VerifyTerraIDToken(tc.token, tc.keys, tc.expect)
			if !errors.Is(err, ErrTerraIDTokenInvalid) {
				t.Fatalf("want ErrTerraIDTokenInvalid, got %v", err)
			}
			if !reflect.DeepEqual(claims, TerraIDClaims{}) {
				t.Fatalf("refused token still returned claims: %+v", claims)
			}
		})
	}

	// Sanity: the base token used above is itself accepted.
	if _, err := VerifyTerraIDToken(good, keys.jwkSet, loginExpect); err != nil {
		t.Fatalf("base token refused: %v", err)
	}
}

func TestTerraIDTypCaseInsensitive(t *testing.T) {
	keys := newTerraIDTestKeys(t)
	token := craftTerraIDToken(t, map[string]any{"alg": "EdDSA", "kid": "ed-1", "typ": "Terra-Assertion+JWT"},
		testTerraIDClaims(t, TerraIDUseNodeEnrollCodeIssue), keys.ed)
	if _, err := VerifyTerraIDToken(token, keys.jwkSet, testTerraIDExpectation(t, TerraIDUseNodeEnrollCodeIssue)); err != nil {
		t.Fatalf("typ is a media type and compares case-insensitively: %v", err)
	}
}

func TestSignTerraIDTokenRejected(t *testing.T) {
	keys := newTerraIDTestKeys(t)
	claims := testTerraIDClaims(t, TerraIDUseTreeLogin)
	if _, err := SignTerraIDToken(claims, "", "", keys.ed); !errors.Is(err, ErrTerraIDKeyInvalid) {
		t.Errorf("empty kid: %v", err)
	}
	if _, err := SignTerraIDToken(claims, "ed-1", "at+jwt", keys.ed); !errors.Is(err, ErrTerraIDTokenInvalid) {
		t.Errorf("unknown typ: %v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if _, err := SignTerraIDToken(claims, "ec-384", "", p384); !errors.Is(err, ErrTerraIDKeyInvalid) {
		t.Errorf("P-384 key: %v", err)
	}
}

func TestParseJWKSet(t *testing.T) {
	keys := newTerraIDTestKeys(t)
	raw, _ := json.Marshal(keys.jwkSet)
	set, err := ParseJWKSet(raw)
	if err != nil {
		t.Fatalf("valid set: %v", err)
	}
	if !reflect.DeepEqual(set, keys.jwkSet) {
		t.Fatalf("set changed: %+v", set)
	}
	// alg and use are optional on a key.
	bare := keys.edJWK
	bare.Alg, bare.Use = "", ""
	if _, err := ParseJWKSet(mustJSON(t, JWKSet{Keys: []JWK{bare}})); err != nil {
		t.Fatalf("key without alg/use: %v", err)
	}

	edPublic := keys.ed.Public().(ed25519.PublicKey)
	ecPoint := func() (string, string) { return keys.ecJWK.X, keys.ecJWK.Y }
	ecX, ecY := ecPoint()
	offCurveY := func() string {
		y, _ := b64.DecodeString(ecY)
		y[31] ^= 0x01
		return b64.EncodeToString(y)
	}()

	cases := map[string]string{
		"not JSON":       `{`,
		"no keys":        `{"keys":[]}`,
		"keys missing":   `{}`,
		"RSA key":        `{"keys":[{"kty":"RSA","kid":"r","n":"AQAB","e":"AQAB"}]}`,
		"oct key":        `{"keys":[{"kty":"oct","kid":"h","k":"c2VjcmV0"}]}`,
		"X25519 key":     `{"keys":[{"kty":"OKP","crv":"X25519","kid":"x","x":"` + b64.EncodeToString(edPublic) + `"}]}`,
		"P-384 key":      `{"keys":[{"kty":"EC","crv":"P-384","kid":"p","x":"` + ecX + `","y":"` + ecY + `"}]}`,
		"missing kid":    `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"` + b64.EncodeToString(edPublic) + `"}]}`,
		"duplicate kid":  `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"a","x":"` + b64.EncodeToString(edPublic) + `"},{"kty":"EC","crv":"P-256","kid":"a","x":"` + ecX + `","y":"` + ecY + `"}]}`,
		"short Ed25519":  `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"a","x":"` + b64.EncodeToString(edPublic[:31]) + `"}]}`,
		"Ed25519 with y": `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"a","x":"` + b64.EncodeToString(edPublic) + `","y":"` + ecY + `"}]}`,
		"padded x":       `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"a","x":"` + b64.EncodeToString(edPublic) + `="}]}`,
		"short P-256 x":  `{"keys":[{"kty":"EC","crv":"P-256","kid":"a","x":"` + ecX[:40] + `","y":"` + ecY + `"}]}`,
		"P-256 no y":     `{"keys":[{"kty":"EC","crv":"P-256","kid":"a","x":"` + ecX + `"}]}`,
		"off curve":      `{"keys":[{"kty":"EC","crv":"P-256","kid":"a","x":"` + ecX + `","y":"` + offCurveY + `"}]}`,
		"alg mismatch":   `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"a","alg":"ES256","x":"` + b64.EncodeToString(edPublic) + `"}]}`,
		"use enc":        `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"a","use":"enc","x":"` + b64.EncodeToString(edPublic) + `"}]}`,
	}
	for name, raw := range cases {
		if _, err := ParseJWKSet([]byte(raw)); !errors.Is(err, ErrTerraIDKeyInvalid) {
			t.Errorf("%s: want ErrTerraIDKeyInvalid, got %v", name, err)
		}
	}
}

func TestJWKFromPublicKey(t *testing.T) {
	keys := newTerraIDTestKeys(t)
	if keys.edJWK.KeyType != JWKKeyTypeOKP || keys.edJWK.Curve != JWKCurveEd25519 || keys.edJWK.Alg != JWSAlgEdDSA ||
		keys.edJWK.Use != JWKUseSignature || keys.edJWK.Y != "" {
		t.Errorf("Ed25519 JWK: %+v", keys.edJWK)
	}
	if keys.ecJWK.KeyType != JWKKeyTypeEC || keys.ecJWK.Curve != JWKCurveP256 || keys.ecJWK.Alg != JWSAlgES256 ||
		len(keys.ecJWK.X) != 43 || len(keys.ecJWK.Y) != 43 {
		t.Errorf("P-256 JWK: %+v", keys.ecJWK)
	}
	pub, err := keys.ecJWK.publicKey()
	if err != nil || !pub.(*ecdsa.PublicKey).Equal(keys.ec.Public()) {
		t.Errorf("P-256 JWK does not round-trip: %v", err)
	}
	if _, err := JWKFromPublicKey("", keys.ed.Public()); !errors.Is(err, ErrTerraIDKeyInvalid) {
		t.Errorf("empty kid: %v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if _, err := JWKFromPublicKey("p", p384.Public()); !errors.Is(err, ErrTerraIDKeyInvalid) {
		t.Errorf("P-384: %v", err)
	}
	if _, err := JWKFromPublicKey("x", "not a key"); !errors.Is(err, ErrTerraIDKeyInvalid) {
		t.Errorf("unknown type: %v", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
