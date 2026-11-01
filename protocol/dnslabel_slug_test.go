package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDNSLabelFromDisplayName(t *testing.T) {
	cases := map[string]string{
		"Marui Desk":                   "marui-desk",
		"rdk-1":                        "rdk-1",
		"  --Lab__PC 02--":             "lab-pc-02",
		"박사님 데스크탑":                     "",
		"박사님 desk":                     "desk",
		"xn--abc":                      "xn-abc",
		"":                             "",
		strings.Repeat("a", 62) + " b": strings.Repeat("a", 62),
	}
	for input, want := range cases {
		got := DNSLabelFromDisplayName(input)
		if got != want {
			t.Errorf("DNSLabelFromDisplayName(%q) = %q, want %q", input, got, want)
		}
		if got != "" {
			if err := ValidateDNSLabel(got); err != nil {
				t.Errorf("slug %q of %q is not a valid label: %v", got, input, err)
			}
		}
	}
}

func TestDNSLabelWithSuffix(t *testing.T) {
	if got := DNSLabelWithSuffix("desk", 1); got != "desk" {
		t.Fatalf("n=1: %q", got)
	}
	if got := DNSLabelWithSuffix("desk", 3); got != "desk-3" {
		t.Fatalf("n=3: %q", got)
	}
	long := strings.Repeat("a", 61) + "-b"
	got := DNSLabelWithSuffix(long, 12)
	if len(got) > 63 || ValidateDNSLabel(got) != nil {
		t.Fatalf("long suffix %q (len %d) is not a valid label", got, len(got))
	}
}

func TestNameRecordSignVerify(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	record := NameRecord{
		FQDN: "rdk.lab.marui.terra", NodeID: "node_a", TreeID: "tree_1",
		IssuedAt: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), TTLSeconds: 300,
	}
	signed := SignNameRecord(record, "k1", private)
	verifier := Ed25519NameVerifier{"k1": public}
	if err := verifier.VerifyNameRecord(signed); err != nil {
		t.Fatalf("verify: %v", err)
	}
	tampered := signed
	tampered.NodeID = "node_evil"
	if err := verifier.VerifyNameRecord(tampered); !errors.Is(err, ErrNameSignature) {
		t.Fatalf("tampered record verified: %v", err)
	}
	if err := (Ed25519NameVerifier{}).VerifyNameRecord(signed); !errors.Is(err, ErrNameSignature) {
		t.Fatalf("unknown key verified: %v", err)
	}
	if got := signed.ExpiresAt(); !got.Equal(record.IssuedAt.Add(5 * time.Minute)) {
		t.Fatalf("expires: %v", got)
	}
}

func TestNameRecordSigningBytesAreStable(t *testing.T) {
	record := NameRecord{
		FQDN: "RDK.lab.marui.terra", NodeID: "node_a", TreeID: "tree_1", TreePublicKey: "pk",
		IssuedAt: time.Date(2026, 10, 29, 9, 0, 0, 0, time.FixedZone("KST", 9*3600)), TTLSeconds: 60, KeyID: "k1",
	}
	want := "terra-name-record/v1\nrdk.lab.marui.terra\nnode_a\ntree_1\npk\n2026-10-29T00:00:00Z\n60\nk1"
	if got := string(record.SigningBytes()); got != want {
		t.Fatalf("signing bytes changed:\n%s\nwant:\n%s", got, want)
	}
	record.TreeOrigin = "https://lab.example.net"
	record.CanonicalFQDN = "RDK.lab.maru.terra"
	if got := string(record.SigningBytes()); got != want+"\ncanonical_fqdn=rdk.lab.maru.terra\ntree_origin=https://lab.example.net" {
		t.Fatalf("optional fields sign as named trailing lines, got:\n%s", got)
	}
}
