package protocol

// What a `.terra` name answers, as carried between the name service, a Tree and
// a client.
//
// The name service (terra-access) is the authority for which node a name points
// at; a Tree is the source of its own nodes' labels; a client keeps what it was
// told so a name still resolves while the service is down. All three handle the
// same record, so it lives here rather than in any one of them — a field that
// one side spelled differently would be a name that resolves in one place and
// not another.
//
// A name answers a node_id, never an address. The path to that node is chosen
// afterwards by the mesh and relay exactly as for any other reference.

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Name resolution failures beyond the reference ones in noderef.go. They are
// distinct because the operator's next move differs: a tombstoned name will not
// come back to this node, an unavailable service may.
var (
	// ErrNameTombstoned is a name that was released and is held so nobody else
	// can take it yet. It no longer points at any node.
	ErrNameTombstoned = errors.New("terra name is tombstoned")
	// ErrNamesUnavailable is a name the service could not be asked about and no
	// cached answer exists for.
	ErrNamesUnavailable = errors.New("terra name service is unavailable")
	// ErrNameSignature is a record whose signature does not verify.
	ErrNameSignature = errors.New("terra name record signature is invalid")
)

// NameRecord is the service's answer for a live name (GET /v1/names/{fqdn}).
type NameRecord struct {
	FQDN   string `json:"fqdn"`
	NodeID string `json:"node_id"`
	TreeID string `json:"tree_id"`
	// TreePublicKey pins the Tree that owns the node. Trust follows this key,
	// not the name: a name that is later reassigned cannot carry it along.
	TreePublicKey string `json:"tree_public_key,omitempty"`
	// TreeOrigin is the owning Tree's public address, when it has one. A
	// client that is not enrolled yet (the installer) has no other way to find
	// the Tree a name belongs to; an enrolled one keeps routing by node_id.
	TreeOrigin string `json:"tree_origin,omitempty"`
	// CanonicalFQDN is set when FQDN is an alias kept after a handle change:
	// the name still answers, and this is the name it answers for now.
	CanonicalFQDN string    `json:"canonical_fqdn,omitempty"`
	IssuedAt      time.Time `json:"issued_at"`
	TTLSeconds    int       `json:"ttl_seconds"`
	KeyID         string    `json:"key_id,omitempty"`
	// Signature is base64 over SigningBytes, by the key KeyID names.
	Signature string `json:"signature,omitempty"`
}

// NameTombstone is the service's answer for a released name (HTTP 410).
type NameTombstone struct {
	FQDN         string    `json:"fqdn"`
	State        string    `json:"state"`
	FormerNodeID string    `json:"former_node_id,omitempty"`
	ReleaseAt    time.Time `json:"release_at"`
}

// NameRecordStateTombstone is NameTombstone.State.
const NameRecordStateTombstone = "tombstone"

// SigningBytes is what Signature covers: every field but the signature, in a
// fixed order, one per line. Kept as plain text rather than canonical JSON so
// that the service, a Tree and a client cannot disagree about encoding.
//
// The optional fields come last, each only when set and each with its name in
// front: a record without them signs exactly as before they existed, and two
// records that set different optional fields can never produce the same bytes.
func (r NameRecord) SigningBytes() []byte {
	lines := []string{
		"terra-name-record/v1",
		strings.ToLower(strings.TrimSpace(r.FQDN)),
		strings.TrimSpace(r.NodeID),
		strings.TrimSpace(r.TreeID),
		strings.TrimSpace(r.TreePublicKey),
		r.IssuedAt.UTC().Format(time.RFC3339),
		strconv.Itoa(r.TTLSeconds),
		strings.TrimSpace(r.KeyID),
	}
	if canonical := strings.ToLower(strings.TrimSpace(r.CanonicalFQDN)); canonical != "" {
		lines = append(lines, "canonical_fqdn="+canonical)
	}
	if origin := strings.TrimSpace(r.TreeOrigin); origin != "" {
		lines = append(lines, "tree_origin="+origin)
	}
	return []byte(strings.Join(lines, "\n"))
}

// ExpiresAt is when the record stops being fresh. A stale record is still the
// best answer while the service cannot be reached — see NameResolver.
func (r NameRecord) ExpiresAt() time.Time {
	return r.IssuedAt.Add(time.Duration(r.TTLSeconds) * time.Second)
}

// Validate checks the shape a record must have before anyone acts on it.
func (r NameRecord) Validate() error {
	if strings.TrimSpace(r.FQDN) == "" {
		return errors.New("name record: fqdn is required")
	}
	if !ValidNodeID(r.NodeID) {
		return fmt.Errorf("name record %q: node_id %q is not a node id", r.FQDN, r.NodeID)
	}
	if r.IssuedAt.IsZero() {
		return fmt.Errorf("name record %q: issued_at is required", r.FQDN)
	}
	if r.TTLSeconds < 0 {
		return fmt.Errorf("name record %q: ttl_seconds must not be negative", r.FQDN)
	}
	return nil
}

// NameVerifier checks a record's signature. Which key signs (the service's or
// the owning Tree's) is the name service's decision; a resolver only needs this.
type NameVerifier interface {
	VerifyNameRecord(record NameRecord) error
}

// Ed25519NameVerifier verifies records against pinned keys by key id.
type Ed25519NameVerifier map[string]ed25519.PublicKey

// VerifyNameRecord implements NameVerifier.
func (v Ed25519NameVerifier) VerifyNameRecord(record NameRecord) error {
	key, ok := v[strings.TrimSpace(record.KeyID)]
	if !ok {
		return fmt.Errorf("%w: unknown key id %q", ErrNameSignature, record.KeyID)
	}
	signature, err := base64.StdEncoding.DecodeString(record.Signature)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNameSignature, err)
	}
	if !ed25519.Verify(key, record.SigningBytes(), signature) {
		return ErrNameSignature
	}
	return nil
}

// SignNameRecord fills Signature with an Ed25519 signature by key. The service
// signs this way; tests and a self-hosted name service may too.
func SignNameRecord(record NameRecord, keyID string, key ed25519.PrivateKey) NameRecord {
	record.KeyID = keyID
	record.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, record.SigningBytes()))
	return record
}

// NameResolver answers the node a `.terra` name points at when the local node
// index does not know it. Implementations consult a cache and the name service;
// Stale reports an answer served from an expired cache entry because the
// service could not be reached.
type NameResolver interface {
	ResolveTerraName(ctx context.Context, fqdn string) (record NameRecord, stale bool, err error)
}

// TreeNameEntry is one label a Tree publishes for one of its nodes. It is the
// element of both the upward summary and the publication to the name service;
// the service composes the fqdn from the Tree's own handle and tree label, so a
// Tree does not need to know them.
type TreeNameEntry struct {
	DNSLabel string `json:"dns_label"`
	NodeID   string `json:"node_id"`
}

// TreeNamePublication is a Tree's full set of names (POST
// /internal/trees/{tree_id}/names). It replaces the previous set: a label that
// is missing was released, and the service tombstones it. Generation only grows;
// a publication older than the last one applied is ignored.
type TreeNamePublication struct {
	TreeID     string          `json:"tree_id"`
	Generation int64           `json:"generation"`
	Names      []TreeNameEntry `json:"names"`
	ObservedAt time.Time       `json:"observed_at"`
}
