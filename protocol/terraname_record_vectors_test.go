package protocol

// Golden vectors for `.terra` name records and default labels, published in the
// vectors package (terra-access runs these same vectors against its own code). Regenerate with `go test -run TestTerraNameRecordVectors -update`
// only when the contract changes on purpose — a changed vector is a changed
// wire format, and every implementation has to move with it. The name grammar's own
// vectors (parsing and normalisation) are P1's, in vectors/terraname/vectors.json.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var updateVectors = flag.Bool("update", false, "rewrite the golden vectors")

const terraNameVectorsPath = "vectors/terraname/record_vectors.json"

type terraNameVectors struct {
	Slug    []slugVector    `json:"slug"`
	Signing []signingVector `json:"signing"`
}

type slugVector struct {
	DisplayName string `json:"display_name"`
	DNSLabel    string `json:"dns_label"`
}

type signingVector struct {
	Record          NameRecord `json:"record"`
	SigningBytesHex string     `json:"signing_bytes_hex"`
	Ed25519SeedHex  string     `json:"ed25519_seed_hex"`
	PublicKeyB64    string     `json:"public_key_b64"`
	SignatureB64    string     `json:"signature_b64"`
}

var slugVectorInputs = []string{
	"Marui Desk", "rdk-1", "  --Lab__PC 02--", "박사님 데스크탑", "박사님 desk", "xn--abc", "",
}

func buildTerraNameVectors(t *testing.T) terraNameVectors {
	t.Helper()
	var vectors terraNameVectors
	for _, input := range slugVectorInputs {
		vectors.Slug = append(vectors.Slug, slugVector{DisplayName: input, DNSLabel: DNSLabelFromDisplayName(input)})
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	key := ed25519.NewKeyFromSeed(seed)
	for _, record := range []NameRecord{
		{FQDN: "rdk.lab.marui.terra", NodeID: "node_1787923476493_4c773555dd1dd13d", TreeID: "tree_01JB",
			TreePublicKey: "MCowBQYDK2VwAyEA", TreeOrigin: "https://lab.example.net",
			CanonicalFQDN: "rdk.lab.maru.terra",
			IssuedAt:      time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), TTLSeconds: 300},
		{FQDN: "desk.marui.terra", NodeID: "node_a", TreeID: "tree_02",
			IssuedAt: time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC), TTLSeconds: 0},
	} {
		signed := SignNameRecord(record, "names-2026-10", key)
		vectors.Signing = append(vectors.Signing, signingVector{
			Record:          signed,
			SigningBytesHex: hex.EncodeToString(signed.SigningBytes()),
			Ed25519SeedHex:  hex.EncodeToString(seed),
			PublicKeyB64:    base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
			SignatureB64:    signed.Signature,
		})
	}
	return vectors
}

func TestTerraNameRecordVectors(t *testing.T) {
	got := buildTerraNameVectors(t)
	if *updateVectors {
		encoded, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(terraNameVectorsPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(terraNameVectorsPath, append(encoded, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(terraNameVectorsPath)
	if err != nil {
		t.Fatalf("read vectors (run with -update to create): %v", err)
	}
	var want terraNameVectors
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("golden vectors drifted from the code; if the contract changed on purpose, regenerate with -update and tell every copy")
	}
	for _, vector := range want.Signing {
		public, _ := base64.StdEncoding.DecodeString(vector.PublicKeyB64)
		if err := (Ed25519NameVerifier{vector.Record.KeyID: public}).VerifyNameRecord(vector.Record); err != nil {
			t.Fatalf("vector %s does not verify: %v", vector.Record.FQDN, err)
		}
	}
}
