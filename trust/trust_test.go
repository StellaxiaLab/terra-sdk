package trust

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleV2 = `{
  "schemaVersion": 2,
  "id": "io.terra.backup",
  "version": "1.0.0",
  "kind": "service",
  "compatibility": {"products": ["leaf", "tree"], "os": ["linux", "windows", "macos"]},
  "entrypoints": {"process": {"linux-amd64": "bin/linux-amd64/terra-backup"}},
  "provides": {"capabilities": [{"id": "io.terra.backup", "version": "1.0.0"}]},
  "contracts": {"api": "contracts/api/terra-api.json"},
  "readiness": {"operationId": "io.terra.backup.health.get", "timeoutMs": 15000},
  "update": {"strategy": "start-before-stop", "drainTimeoutMs": 30000, "rollback": true}
}`

// TestRevocationDigestNotationsAreEquivalent: a digest-scoped revocation must
// bite whichever notation the market stored ("sha256:<hex>" or "<hex>", any
// case) and whichever the node asks with — the install gate passes
// "sha256:<hex>", the installed re-evaluation passes bare hex.
func TestRevocationDigestNotationsAreEquivalent(t *testing.T) {
	const hex = "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12"
	now := time.Now().UTC()
	notations := []string{hex, "sha256:" + hex, "SHA256:" + "AB12CD34EF56AB12CD34EF56AB12CD34EF56AB12CD34EF56AB12CD34EF56AB12"}
	for _, stored := range notations {
		snapshot := RevocationSnapshot{MarketID: "terra", Sequence: 1, IssuedAt: now.Format(time.RFC3339),
			ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
			Revoked:   []RevokedEntry{{ID: "com.acme.d", Version: "*", Digest: stored, Reason: RevokeSecurity}}}
		for _, asked := range notations {
			if verdict := snapshot.Judge("com.acme.d", "1.0.0", asked); !verdict.Revoked {
				t.Errorf("stored %q, asked %q: not revoked", stored, asked)
			}
			gate := MarketGate{Snapshot: &snapshot, Now: now}
			if err := gate.Check("com.acme.d", "1.0.0", asked); !errors.Is(err, ErrRevoked) {
				t.Errorf("gate: stored %q, asked %q: %v", stored, asked, err)
			}
		}

		if snapshot.Judge("com.acme.d", "1.0.0", "sha256:"+hex[:62]+"00").Revoked {
			t.Errorf("stored %q revoked a different digest", stored)
		}
	}
}

// The tree (distribution) path's gate tolerates a
// missing or expired snapshot but still refuses what a snapshot revokes. The
// direct path's gate (AllowStale unset) is unchanged: fail-closed.
func TestMarketGateAllowStale(t *testing.T) {
	now := time.Now().UTC()
	expired := &RevocationSnapshot{MarketID: "terra", Sequence: 1,
		IssuedAt: now.Add(-48 * time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339),
		Revoked: []RevokedEntry{{ID: "com.acme.bad", Version: "1.0.0", Reason: RevokeSecurity}}}

	for _, c := range []struct {
		name string
		gate MarketGate
		id   string
		want error
	}{
		{"direct, no snapshot", MarketGate{Now: now}, "com.acme.ok", ErrRevocationStale},
		{"direct, expired snapshot", MarketGate{Snapshot: expired, Now: now}, "com.acme.ok", ErrRevocationStale},
		{"tree, no snapshot", MarketGate{Now: now, AllowStale: true}, "com.acme.ok", nil},
		{"tree, expired snapshot", MarketGate{Snapshot: expired, Now: now, AllowStale: true}, "com.acme.ok", nil},
		{"tree, expired snapshot that revokes it", MarketGate{Snapshot: expired, Now: now, AllowStale: true}, "com.acme.bad", ErrRevoked},
	} {
		err := c.gate.Check(c.id, "1.0.0", "sha256:00")
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

var marketNow = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

type marketFixture struct {
	root        MarketRoot
	rootPrivs   []ed25519.PrivateKey
	attestPriv  ed25519.PrivateKey
	attestCert  MarketKeyCertificate
	revokePriv  ed25519.PrivateKey
	revokeCert  MarketKeyCertificate
	raw         []byte
	attestation CounterSignature
}

func seedKey(label string) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	copy(seed, label)
	return ed25519.NewKeyFromSeed(seed)
}

func (f *marketFixture) certify(t *testing.T, role string, priv ed25519.PrivateKey, notBefore, notAfter string, signers int) MarketKeyCertificate {
	t.Helper()
	public := priv.Public().(ed25519.PublicKey)
	cert := MarketKeyCertificate{Role: role, KeyID: TrustKeyID(public), PublicKey: b64(public), Epoch: 1, NotBefore: notBefore, NotAfter: notAfter}
	for i := 0; i < signers; i++ {
		sig, err := SignMarketCertificate(f.root.MarketID, cert, f.rootPrivs[i], "2026-10-01T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		cert.Signatures = append(cert.Signatures, sig)
	}
	return cert
}

func b64(p []byte) string { return base64.RawURLEncoding.EncodeToString(p) }

func newMarketFixture(t *testing.T) *marketFixture {
	t.Helper()
	f := &marketFixture{}
	f.root = MarketRoot{MarketID: "terra", Threshold: 2}
	for _, label := range []string{"root-a", "root-b", "root-c"} {
		priv := seedKey(label)
		f.rootPrivs = append(f.rootPrivs, priv)
		pub := priv.Public().(ed25519.PublicKey)
		f.root.Keys = append(f.root.Keys, TrustKey{KeyID: TrustKeyID(pub), PublicKey: pub})
	}
	f.attestPriv, f.revokePriv = seedKey("attest-1"), seedKey("revoke-1")
	f.attestCert = f.certify(t, MarketRoleAttest, f.attestPriv, "2026-10-01T00:00:00Z", "2027-10-01T00:00:00Z", 2)
	f.revokeCert = f.certify(t, MarketRoleRevoke, f.revokePriv, "2026-10-01T00:00:00Z", "2027-10-01T00:00:00Z", 2)
	f.raw, _, _, _ = counterSignFixture(t)
	att, err := CounterSignManifest(f.raw, f.attestPriv, "market:terra", "2026-10-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	cert := f.attestCert
	att.Cert = &cert
	f.attestation = att
	return f
}

func TestMarketAttestationGradesTrustedThirdParty(t *testing.T) {
	f := newMarketFixture(t)
	result := VerifyChain(f.raw, []CounterSignature{f.attestation}, ChainAnchors{Market: &f.root}, marketNow)
	if result.Tier != TrustTrustedThirdParty || !result.HasMarket() || result.Issuer != "market:terra" {
		t.Fatalf("not graded: %+v", result)
	}
}

func TestMarketAndTreeBothRecorded(t *testing.T) {
	f := newMarketFixture(t)
	raw, treeChain, anchor, _ := counterSignFixture(t)
	f.raw = raw
	att, _ := CounterSignManifest(raw, f.attestPriv, "market:terra", "")
	cert := f.attestCert
	att.Cert = &cert
	result := VerifyChain(raw, append([]CounterSignature{att}, treeChain...), ChainAnchors{Tree: []TrustKey{anchor}, Market: &f.root}, marketNow)
	if result.Tier != TrustTrustedThirdParty || len(result.Attestations) != 2 {
		t.Fatalf("want both recorded: %+v", result)
	}
	if got := result.Provenance(); got[0] != "market:terra" || got[1] != "tree:node_parent_1" {
		t.Fatalf("provenance %v", got)
	}
}

func TestMarketThresholdRoot(t *testing.T) {
	f := newMarketFixture(t)
	one := f.certify(t, MarketRoleAttest, f.attestPriv, "2026-10-01T00:00:00Z", "2027-10-01T00:00:00Z", 1)
	if _, err := VerifyMarketCertificate(f.root, one, MarketRoleAttest, marketNow); !errors.Is(err, ErrMarketCertInvalid) {
		t.Fatalf("1 of 2 must fail: %v", err)
	}

	dup := one
	dup.Signatures = append(dup.Signatures, one.Signatures[0])
	if _, err := VerifyMarketCertificate(f.root, dup, MarketRoleAttest, marketNow); err == nil {
		t.Fatal("duplicate signer counted twice")
	}

	two := f.attestCert
	two.Signatures = nil
	for _, i := range []int{1, 2} {
		sig, _ := SignMarketCertificate("terra", two, f.rootPrivs[i], "")
		two.Signatures = append(two.Signatures, sig)
	}
	if _, err := VerifyMarketCertificate(f.root, two, MarketRoleAttest, marketNow); err != nil {
		t.Fatalf("2 of 3 failed: %v", err)
	}

	rogue := f.attestCert
	rogue.Signatures = nil
	for _, key := range []ed25519.PrivateKey{seedKey("rogue-1"), seedKey("rogue-2")} {
		sig, _ := SignMarketCertificate("terra", rogue, key, "")
		rogue.Signatures = append(rogue.Signatures, sig)
	}
	if _, err := VerifyMarketCertificate(f.root, rogue, MarketRoleAttest, marketNow); err == nil {
		t.Fatal("rogue signers accepted")
	}
}

func TestMarketCertExpiryRoleAndTamper(t *testing.T) {
	f := newMarketFixture(t)
	if _, err := VerifyMarketCertificate(f.root, f.attestCert, MarketRoleAttest, marketNow.AddDate(2, 0, 0)); !errors.Is(err, ErrMarketCertExpired) {
		t.Fatalf("expired: %v", err)
	}
	if _, err := VerifyMarketCertificate(f.root, f.attestCert, MarketRoleAttest, marketNow.AddDate(-1, 0, 0)); !errors.Is(err, ErrMarketCertExpired) {
		t.Fatalf("not yet valid: %v", err)
	}
	if _, err := VerifyMarketCertificate(f.root, f.attestCert, MarketRoleRevoke, marketNow); err == nil {
		t.Fatal("attest cert accepted as revoke")
	}
	tampered := f.attestCert
	tampered.NotAfter = "2099-01-01T00:00:00Z"
	if _, err := VerifyMarketCertificate(f.root, tampered, MarketRoleAttest, marketNow); err == nil {
		t.Fatal("tampered window accepted")
	}

	result := VerifyChain(f.raw, []CounterSignature{f.attestation}, ChainAnchors{Market: &f.root}, marketNow.AddDate(2, 0, 0))
	if result.Endorsed() {
		t.Fatal("expired cert endorsed a package")
	}

	other := f.root
	other.MarketID = "evil"
	if _, err := VerifyMarketCertificate(other, f.attestCert, MarketRoleAttest, marketNow); err == nil {
		t.Fatal("cert verified under another market id")
	}
}

func TestMarketForgedChain(t *testing.T) {
	f := newMarketFixture(t)

	rogue := seedKey("rogue-attest")
	cert := f.certify(t, MarketRoleAttest, rogue, "2026-10-01T00:00:00Z", "2027-10-01T00:00:00Z", 0)
	att, _ := CounterSignManifest(f.raw, rogue, "market:terra", "")
	att.Cert = &cert
	if VerifyChain(f.raw, []CounterSignature{att}, ChainAnchors{Market: &f.root}, marketNow).Endorsed() {
		t.Fatal("uncertified key endorsed")
	}

	other, _, _, _ := counterSignFixture(t)
	if VerifyChain(other, []CounterSignature{f.attestation}, ChainAnchors{Market: &f.root}, marketNow).Endorsed() {
		t.Fatal("attestation replayed onto another manifest")
	}

	swapped := f.attestation
	swapped.Issuer = "market:other"
	if VerifyChain(f.raw, []CounterSignature{swapped}, ChainAnchors{Market: &f.root}, marketNow).Endorsed() {
		t.Fatal("issuer swap endorsed")
	}

	bare := f.attestation
	bare.Cert = nil
	if VerifyChain(f.raw, []CounterSignature{bare}, ChainAnchors{Market: &f.root}, marketNow).Endorsed() {
		t.Fatal("entry without cert endorsed")
	}

	anchor := TrustKey{KeyID: f.attestation.KeyID, Issuer: "market:terra", PublicKey: f.attestPriv.Public().(ed25519.PublicKey)}
	if VerifyChain(f.raw, []CounterSignature{f.attestation}, ChainAnchors{Tree: []TrustKey{anchor}}, marketNow).Endorsed() {
		t.Fatal("market entry accepted through tree anchors")
	}
}

func (f *marketFixture) snapshot(t *testing.T, seq int64, expires string, revoked ...RevokedEntry) RevocationSnapshot {
	t.Helper()
	s, err := SignSnapshot(RevocationSnapshot{
		MarketID: "terra", Sequence: seq, IssuedAt: "2026-10-10T00:00:00Z", ExpiresAt: expires,
		Revoked: revoked, Latest: map[string]string{"com.acme.hello": "1.2.0"},
	}, f.revokePriv, f.revokeCert)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSnapshotVerifyRollbackExpiry(t *testing.T) {
	f := newMarketFixture(t)
	s5 := f.snapshot(t, 5, "2026-10-17T00:00:00Z")
	if err := VerifySnapshot(f.root, s5, marketNow); err != nil {
		t.Fatal(err)
	}
	if err := AcceptSnapshot(f.root, nil, s5, marketNow); err != nil {
		t.Fatal(err)
	}
	if err := AcceptSnapshot(f.root, &s5, f.snapshot(t, 4, "2026-10-17T00:00:00Z"), marketNow); !errors.Is(err, ErrSnapshotRollback) {
		t.Fatalf("rollback: %v", err)
	}
	if err := AcceptSnapshot(f.root, &s5, f.snapshot(t, 5, "2026-10-17T00:00:00Z"), marketNow); !errors.Is(err, ErrSnapshotSame) {
		t.Fatalf("same: %v", err)
	}
	if err := AcceptSnapshot(f.root, &s5, f.snapshot(t, 6, "2026-10-17T00:00:00Z"), marketNow); err != nil {
		t.Fatalf("forward: %v", err)
	}
	if err := AcceptSnapshot(f.root, &s5, f.snapshot(t, 7, "2026-10-01T00:00:00Z"), marketNow); !errors.Is(err, ErrSnapshotExpiredNew) {
		t.Fatalf("expired new: %v", err)
	}
	if !s5.Fresh(marketNow) || s5.Fresh(marketNow.AddDate(0, 0, 30)) {
		t.Fatal("Fresh wrong")
	}

	bad := s5
	bad.Latest = map[string]string{"com.acme.hello": "0.0.1"}
	if err := VerifySnapshot(f.root, bad, marketNow); !errors.Is(err, ErrSnapshotSignature) {
		t.Fatalf("tamper: %v", err)
	}
	wrongRole, err := SignSnapshot(RevocationSnapshot{MarketID: "terra", Sequence: 9, ExpiresAt: "2026-10-17T00:00:00Z"}, f.attestPriv, f.attestCert)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySnapshot(f.root, wrongRole, marketNow); !errors.Is(err, ErrSnapshotSignature) {
		t.Fatalf("attest key signed a snapshot: %v", err)
	}
}

func TestMarketGateAndDowngrade(t *testing.T) {
	f := newMarketFixture(t)
	s := f.snapshot(t, 1, "2026-10-17T00:00:00Z", RevokedEntry{ID: "a.b", Version: "1.0.0", Reason: RevokePolicy})
	gate := MarketGate{Snapshot: &s, Now: marketNow}
	if err := gate.Check("x.y", "1.0.0", ""); err != nil {
		t.Fatal(err)
	}
	if err := gate.Check("a.b", "1.0.0", ""); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked: %v", err)
	}
	if err := (MarketGate{Now: marketNow}).Check("x.y", "1", ""); !errors.Is(err, ErrRevocationStale) {
		t.Fatalf("no snapshot: %v", err)
	}
	if err := (MarketGate{Snapshot: &s, Now: marketNow.AddDate(0, 1, 0)}).Check("x.y", "1", ""); !errors.Is(err, ErrRevocationStale) {
		t.Fatalf("expired: %v", err)
	}
	down := MarketGate{Snapshot: &s, Now: marketNow, InstalledSelected: "1.2.0"}
	if err := down.Check("x.y", "1.1.0", ""); !errors.Is(err, ErrDowngrade) {
		t.Fatalf("downgrade: %v", err)
	}
	down.Pinned = true
	if err := down.Check("x.y", "1.1.0", ""); err != nil {
		t.Fatalf("pinned downgrade: %v", err)
	}
}

type marketVectors struct {
	Note            string           `json:"note"`
	ManifestJSON    string           `json:"manifest"`
	AuthorPayload   string           `json:"manifestPayloadHex"`
	CounterPayload  string           `json:"counterSignPayloadHex"`
	AttestKeySeed   string           `json:"attestKeySeedHex"`
	AttestPublicKey string           `json:"attestPublicKey"`
	AttestKeyID     string           `json:"attestKeyId"`
	Attestation     CounterSignature `json:"attestation"`
	Root            struct {
		MarketID  string   `json:"marketId"`
		Threshold int      `json:"threshold"`
		KeyIDs    []string `json:"keyIds"`
	} `json:"root"`
	SnapshotPayload string             `json:"snapshotPayloadHex"`
	Snapshot        RevocationSnapshot `json:"snapshot"`
}

func TestMarketVectors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "market_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var committed marketVectors
	if err := json.Unmarshal(data, &committed); err != nil {
		t.Fatal(err)
	}
	raw := []byte(committed.ManifestJSON)
	// Canonical payload bytes are the cross-language contract.
	author, err := CanonicalManifestPayload(raw)
	if err != nil || hex.EncodeToString(author) != committed.AuthorPayload {
		t.Fatalf("author payload drifted from the committed vector: %v", err)
	}
	counter, err := CanonicalCounterSignPayload(raw)
	if err != nil || hex.EncodeToString(counter) != committed.CounterPayload {
		t.Fatalf("counter-sign payload drifted from the committed vector: %v", err)
	}
	snap, err := CanonicalSnapshotPayload(committed.Snapshot)
	if err != nil || hex.EncodeToString(snap) != committed.SnapshotPayload {
		t.Fatalf("snapshot payload drifted from the committed vector: %v", err)
	}
	// Signer side: the committed seed reproduces the committed key id and the
	// committed attestation signature (Ed25519 is deterministic).
	seed, err := hex.DecodeString(committed.AttestKeySeed)
	if err != nil {
		t.Fatal(err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	if TrustKeyID(pub) != committed.AttestKeyID || b64(pub) != committed.AttestPublicKey {
		t.Fatal("attestation key id or public key drifted from the committed vector")
	}
	again, err := CounterSignManifest(raw, priv, committed.Attestation.Issuer, committed.Attestation.SignedAt)
	if err != nil || again.Value != committed.Attestation.Value || again.KeyID != committed.Attestation.KeyID {
		t.Fatalf("re-signing the vector manifest changed the signature: %v", err)
	}
	// Verifier side.
	root := MarketRoot{MarketID: committed.Root.MarketID, Threshold: committed.Root.Threshold}
	for _, label := range []string{"root-a", "root-b", "root-c"} {
		pub := seedKey(label).Public().(ed25519.PublicKey)
		root.Keys = append(root.Keys, TrustKey{KeyID: TrustKeyID(pub), PublicKey: pub})
	}
	if !VerifyChain(raw, []CounterSignature{committed.Attestation}, ChainAnchors{Market: &root}, marketNow).HasMarket() {
		t.Fatal("committed attestation vector no longer verifies")
	}
	if err := VerifySnapshot(root, committed.Snapshot, marketNow); err != nil {
		t.Fatalf("committed snapshot vector: %v", err)
	}
}

// TestVectorFileIsPinned fails when the golden vector file changes without
// testdata/vectors.sha256 being updated in the same change.
func TestVectorFileIsPinned(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "market_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := os.ReadFile(filepath.Join("testdata", "vectors.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	want := strings.Fields(string(pinned))
	if len(want) == 0 || hex.EncodeToString(sum[:]) != want[0] {
		t.Fatalf("market_vectors.json sha256 %x does not match testdata/vectors.sha256", sum)
	}
}

func TestParseSemverAcceptsPartialAndPrerelease(t *testing.T) {
	cases := map[string]semver{
		"1":            {major: 1},
		"1.2":          {major: 1, minor: 2},
		"1.2.3":        {major: 1, minor: 2, patch: 3},
		"v2.0.0":       {major: 2},
		"1.2.3+build7": {major: 1, minor: 2, patch: 3},
		"1.2.3-rc.1":   {major: 1, minor: 2, patch: 3, prerelease: []string{"rc", "1"}},
	}
	for input, want := range cases {
		got, ok := parseSemver(input)
		if !ok {
			t.Fatalf("parseSemver(%q) failed", input)
		}
		if got.major != want.major || got.minor != want.minor || got.patch != want.patch {
			t.Fatalf("parseSemver(%q) = %+v, want %+v", input, got, want)
		}
		if len(got.prerelease) != len(want.prerelease) {
			t.Fatalf("parseSemver(%q) prerelease = %v, want %v", input, got.prerelease, want.prerelease)
		}
	}
	for _, bad := range []string{"", "x", "1.2.3.4", "1.-2.0", "1.2.a"} {
		if _, ok := parseSemver(bad); ok {
			t.Fatalf("parseSemver(%q) should fail", bad)
		}
	}
}

func TestCompareSemverOrdersReleasesAndPrereleases(t *testing.T) {
	assertLess := func(a, b string) {
		t.Helper()
		va, _ := parseSemver(a)
		vb, _ := parseSemver(b)
		if compareSemver(va, vb) >= 0 {
			t.Fatalf("expected %q < %q", a, b)
		}
		if compareSemver(vb, va) <= 0 {
			t.Fatalf("expected %q > %q", b, a)
		}
	}
	assertLess("1.0.0", "2.0.0")
	assertLess("1.2.0", "1.2.1")
	assertLess("1.0.0-rc.1", "1.0.0")
	assertLess("1.0.0-alpha", "1.0.0-beta")
	assertLess("1.0.0-alpha.1", "1.0.0-alpha.2")
	assertLess("1.0.0-1", "1.0.0-alpha")

	va, _ := parseSemver("1.2.3")
	vb, _ := parseSemver("1.2.3")
	if compareSemver(va, vb) != 0 {
		t.Fatalf("equal versions must compare equal")
	}
}

// newTrustKey generates an Ed25519 keypair and the matching Built-in TrustKey.
func newTrustKey(t *testing.T, issuer string, tier TrustTier) (ed25519.PrivateKey, TrustKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return private, TrustKey{KeyID: TrustKeyID(public), Issuer: issuer, Tier: tier, PublicKey: public}
}

// signedManifest signs raw with private/issuer and returns the manifest bytes
// with the signature block embedded.
func signedManifest(t *testing.T, raw []byte, private ed25519.PrivateKey, issuer string) []byte {
	t.Helper()
	sig, err := SignManifest(raw, private, issuer)
	if err != nil {
		t.Fatalf("sign manifest: %v", err)
	}
	signed, err := EmbedSignature(raw, sig)
	if err != nil {
		t.Fatalf("embed signature: %v", err)
	}
	return signed
}

func TestTrustKeyIDIsStable(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if TrustKeyID(public) != TrustKeyID(public) {
		t.Fatal("TrustKeyID must be deterministic")
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if TrustKeyID(public) == TrustKeyID(other) {
		t.Fatal("different keys must not share a keyId")
	}
}

// counterSignFixture: an author-signed manifest counter-signed by one "tree".
func counterSignFixture(t *testing.T) (raw []byte, chain []CounterSignature, anchor TrustKey, treeKey ed25519.PrivateKey) {
	t.Helper()
	authorKey, _ := newTrustKey(t, "author", TrustBuiltin)
	raw = signedManifest(t, []byte(sampleV2), authorKey, "author")
	var err error
	treePublic, treePrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := CounterSignManifest(raw, treePrivate, "tree:node_parent_1", "2026-08-26T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	anchor = TrustKey{KeyID: TrustKeyID(treePublic), Issuer: "tree:node_parent_1", Tier: TrustTreePublished, PublicKey: treePublic}
	return raw, []CounterSignature{entry}, anchor, treePrivate
}

// TestCounterSignPayloadExcludesChain: hops accrete without invalidating each
// other — a chain already carrying entries verifies identically.
func TestCounterSignPayloadExcludesChain(t *testing.T) {
	raw, chain, anchor, treeKey := counterSignFixture(t)
	// Embed the first hop's chain INTO the manifest (as a hostile or naive
	// intermediary might) — the canonical payload must strip it.
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	signature := generic["signature"].(map[string]any)
	encoded, _ := json.Marshal(chain)
	var chainValue []any
	_ = json.Unmarshal(encoded, &chainValue)
	signature["chain"] = chainValue
	withChain, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyChain(withChain, chain, ChainAnchors{Tree: []TrustKey{anchor}}, time.Now()).Endorsed() {
		t.Fatal("an embedded chain changed the covered payload — hops cannot accrete")
	}

	second, err := CounterSignManifest(withChain, treeKey, "tree:node_parent_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyChain(raw, append(chain, second), ChainAnchors{Tree: []TrustKey{anchor}}, time.Now()).Endorsed() {
		t.Fatal("second hop broke the chain")
	}
}
