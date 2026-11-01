package protocol

// The fixed Terra ID fixture in vectors/terraid is a contract, not a test
// detail: the private terra-id and terra-access repositories read the same
// files and must reach the same verdicts. So this test checks the files as
// they are on disk — it never regenerates them silently — and the fixture
// clock is fixed so the verdicts do not drift with the calendar.
//
// Ed25519 is deterministic, so re-signing the stored claims with the stored
// key must reproduce the stored tokens byte for byte. That is what catches a
// change to claim serialization (field order, omitempty, aud shape) that would
// make this package's tokens differ from what the service produces.
//
// The files were produced once by a throwaway generator (seed =
// sha256("terra-protocol terraid fixture key v1"), iat 2026-10-29T00:00:00Z)
// and are edited only on purpose; this package defines no test flags.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const terraIDFixtureDir = "vectors/terraid"

type terraIDFixtureKey struct {
	Comment   string `json:"comment"`
	Alg       string `json:"alg"`
	KeyID     string `json:"kid"`
	SeedHex   string `json:"seed_hex"`
	PublicJWK JWK    `json:"public_jwk"`
}

type terraIDFixtureToken struct {
	Name   string        `json:"name"`
	KeyID  string        `json:"kid"`
	Typ    string        `json:"typ"`
	Claims TerraIDClaims `json:"claims"`
	Token  string        `json:"token"`
}

type terraIDFixtureExpectation struct {
	Issuer                string    `json:"issuer"`
	Audience              string    `json:"audience"`
	Use                   string    `json:"use"`
	Now                   time.Time `json:"now"`
	RequireTerraIDSubject bool      `json:"require_terra_id_subject"`
}

func (e terraIDFixtureExpectation) expectation() TerraIDExpectation {
	return TerraIDExpectation{Issuer: e.Issuer, Audience: e.Audience, Use: e.Use, Now: e.Now,
		RequireTerraIDSubject: e.RequireTerraIDSubject}
}

type terraIDFixtureCase struct {
	Name   string                    `json:"name"`
	Token  string                    `json:"token"` // a terraIDFixtureToken name
	Expect terraIDFixtureExpectation `json:"expect"`
	Valid  bool                      `json:"valid"`
}

type terraIDFixtureTokens struct {
	Comment string                `json:"comment"`
	Tokens  []terraIDFixtureToken `json:"tokens"`
	Cases   []terraIDFixtureCase  `json:"cases"`
}

func TestTerraIDFixture(t *testing.T) {
	var key terraIDFixtureKey
	readTerraIDFixture(t, "test_key.json", &key)
	seed, err := hex.DecodeString(key.SeedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("seed_hex: %v (len %d)", err, len(seed))
	}
	private := ed25519.NewKeyFromSeed(seed)
	derived, err := JWKFromPublicKey(key.KeyID, private.Public())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(derived, key.PublicJWK) {
		t.Fatalf("public_jwk does not match the seed:\n got %+v\nwant %+v", key.PublicJWK, derived)
	}

	rawJWKS, err := os.ReadFile(filepath.Join(terraIDFixtureDir, "jwks.json"))
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := ParseJWKSet(rawJWKS)
	if err != nil {
		t.Fatalf("jwks.json: %v", err)
	}
	if !reflect.DeepEqual(jwks, JWKSet{Keys: []JWK{key.PublicJWK}}) {
		t.Fatalf("jwks.json is not exactly the test key: %+v", jwks)
	}

	var fixture terraIDFixtureTokens
	readTerraIDFixture(t, "tokens.json", &fixture)
	byName := map[string]terraIDFixtureToken{}
	for _, token := range fixture.Tokens {
		resigned, err := SignTerraIDToken(token.Claims, token.KeyID, token.Typ, private)
		if err != nil {
			t.Fatalf("%s: sign: %v", token.Name, err)
		}
		if resigned != token.Token {
			t.Errorf("%s: re-signing the stored claims gives a different token:\n got %s\nwant %s",
				token.Name, resigned, token.Token)
		}
		byName[token.Name] = token
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("tokens.json has no cases")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			token, ok := byName[tc.Token]
			if !ok {
				t.Fatalf("case names unknown token %q", tc.Token)
			}
			claims, err := VerifyTerraIDToken(token.Token, jwks, tc.Expect.expectation())
			if tc.Valid {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				if !reflect.DeepEqual(claims, token.Claims) {
					t.Fatalf("claims:\n got %+v\nwant %+v", claims, token.Claims)
				}
				return
			}
			if !errors.Is(err, ErrTerraIDTokenInvalid) {
				t.Fatalf("want ErrTerraIDTokenInvalid, got %v", err)
			}
		})
	}

	var bundle TerraIDKeyBundle
	readTerraIDFixture(t, "key_bundle.json", &bundle)
	bundleKeys, err := bundle.KeysFor(fixture.Cases[0].Expect.Issuer, fixture.Cases[0].Expect.Now)
	if err != nil {
		t.Fatalf("key_bundle.json: %v", err)
	}
	if !reflect.DeepEqual(bundleKeys, jwks) {
		t.Fatalf("key_bundle.json keys differ from jwks.json")
	}
	if err := ValidateTerraIDKeyBundle(bundle, bundle.NotAfter); !errors.Is(err, ErrTerraIDKeyBundleInvalid) {
		t.Fatalf("bundle at not_after: %v", err)
	}
}

func readTerraIDFixture(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(terraIDFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}
