package protocol

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func testTerraIDKeyBundle(t *testing.T) TerraIDKeyBundle {
	keys := newTerraIDTestKeys(t)
	return TerraIDKeyBundle{
		Version:  TerraIDKeyBundleVersion,
		Issuer:   testTerraIDIssuer,
		Keys:     keys.jwkSet,
		IssuedAt: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
		NotAfter: time.Date(2027, 4, 29, 0, 0, 0, 0, time.UTC),
	}
}

func TestTerraIDKeyBundleValid(t *testing.T) {
	bundle := testTerraIDKeyBundle(t)
	if err := ValidateTerraIDKeyBundle(bundle, testTerraIDNow); err != nil {
		t.Fatalf("valid bundle: %v", err)
	}
	keys, err := bundle.KeysFor(testTerraIDIssuer, testTerraIDNow)
	if err != nil || len(keys.Keys) != 2 {
		t.Fatalf("KeysFor: %v %v", keys, err)
	}

	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"version", "issuer", "keys", "issued_at", "not_after"} {
		if _, ok := members[name]; !ok {
			t.Errorf("member %q missing from %s", name, raw)
		}
	}
	if string(members["issued_at"]) != `"2026-10-29T00:00:00Z"` {
		t.Errorf("issued_at is %s, want RFC 3339", members["issued_at"])
	}
	if _, err := ParseJWKSet(members["keys"]); err != nil {
		t.Errorf("keys member is not a JWKS document: %v", err)
	}
}

func TestTerraIDKeyBundleRejected(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*TerraIDKeyBundle)
		now    time.Time
		key    bool // the failure is about a key, so ErrTerraIDKeyInvalid is also wrapped
	}{
		{"expired", func(*TerraIDKeyBundle) {}, time.Date(2027, 4, 29, 0, 0, 0, 0, time.UTC), false},
		{"long expired", func(*TerraIDKeyBundle) {}, time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{"version 0", func(b *TerraIDKeyBundle) { b.Version = 0 }, testTerraIDNow, false},
		{"version 2", func(b *TerraIDKeyBundle) { b.Version = 2 }, testTerraIDNow, false},
		{"issuer empty", func(b *TerraIDKeyBundle) { b.Issuer = "" }, testTerraIDNow, false},
		{"issuer trailing slash", func(b *TerraIDKeyBundle) { b.Issuer = testTerraIDIssuer + "/" }, testTerraIDNow, false},
		{"issuer plain http", func(b *TerraIDKeyBundle) { b.Issuer = "http://id.terra.example" }, testTerraIDNow, false},
		{"no keys", func(b *TerraIDKeyBundle) { b.Keys = JWKSet{} }, testTerraIDNow, true},
		{"duplicate kid", func(b *TerraIDKeyBundle) { b.Keys.Keys[1].KeyID = b.Keys.Keys[0].KeyID }, testTerraIDNow, true},
		{"unknown kty", func(b *TerraIDKeyBundle) { b.Keys.Keys[0].KeyType = "RSA" }, testTerraIDNow, true},
		{"issued_at missing", func(b *TerraIDKeyBundle) { b.IssuedAt = time.Time{} }, testTerraIDNow, false},
		{"not_after missing", func(b *TerraIDKeyBundle) { b.NotAfter = time.Time{} }, testTerraIDNow, false},
		{"not_after before issued_at", func(b *TerraIDKeyBundle) { b.NotAfter = b.IssuedAt.Add(-time.Hour) }, testTerraIDNow, false},
		{"not_after equals issued_at", func(b *TerraIDKeyBundle) { b.NotAfter = b.IssuedAt }, testTerraIDNow, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := testTerraIDKeyBundle(t)
			tc.mutate(&bundle)
			err := ValidateTerraIDKeyBundle(bundle, tc.now)
			if !errors.Is(err, ErrTerraIDKeyBundleInvalid) {
				t.Fatalf("want ErrTerraIDKeyBundleInvalid, got %v", err)
			}
			if tc.key && !errors.Is(err, ErrTerraIDKeyInvalid) {
				t.Fatalf("key failure should also wrap ErrTerraIDKeyInvalid, got %v", err)
			}
		})
	}
}

func TestTerraIDKeyBundleIssuerMismatch(t *testing.T) {
	bundle := testTerraIDKeyBundle(t)
	for _, issuer := range []string{"https://id.other.example", testTerraIDIssuer + "/", "https://ID.terra.example", ""} {
		if _, err := bundle.KeysFor(issuer, testTerraIDNow); !errors.Is(err, ErrTerraIDKeyBundleInvalid) {
			t.Errorf("KeysFor(%q): %v", issuer, err)
		}
	}
	// An expired bundle yields no keys even for the right issuer.
	if _, err := bundle.KeysFor(testTerraIDIssuer, bundle.NotAfter); !errors.Is(err, ErrTerraIDKeyBundleInvalid) {
		t.Errorf("expired KeysFor: %v", err)
	}
}
