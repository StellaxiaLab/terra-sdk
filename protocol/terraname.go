package protocol

// How a node is reached by a name that works outside its cluster.
//
// The three names in noderef.go are all scoped. A display name is unique only
// inside a cluster, a cluster address only means something on that cluster's
// network, and a node id, while global, is not something anyone types. That is
// enough while the person typing is inside the cluster. It stops being enough
// the moment the hosted access service lets somebody reach a node from outside
// it: they need one string that says which person's which Tree's which node, and
// that every layer reads the same way.
//
// A terra name is that string: `<label>.<handle>.terra` or
// `<label>.<tree>.<handle>.terra`. It borrows DNS syntax on purpose. The shape
// is already understood by every tool that will end up holding one — a shell, a
// URL bar, a TLS certificate, a log grep — and choosing anything else would mean
// teaching each of them a new escape rule.
//
// This file is only the grammar (P1). Which node a terra name points at — slug
// derivation, the index that answers it, the Master and CLI wiring, the cache —
// belongs to the name part (P4) and is deliberately absent here. A grammar that
// also resolved would have to know where the index lives, and this package must
// not.
//
// A terra name is not a fourth name stored on NodeIdentity. It is a way to reach
// the node, assembled from parts other systems own (the handle from Terra ID, the
// label and tree from the index), and it answers a node_id exactly like the other
// names do.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TerraNameTLD is the last label of every terra name.
//
// It is a private top-level label, not a delegated DNS zone. Reserving it in
// ClassifyNodeRef and ValidateDisplayName is what lets a reference ending in it
// be read as a terra name without asking anyone, the same way the node_ prefix
// lets a reference be read as an id.
const TerraNameTLD = "terra"

// terraNameSuffix is the TLD as it appears at the end of a reference.
const terraNameSuffix = "." + TerraNameTLD

// maxTerraNameLength is the DNS bound on a whole name without its trailing dot.
//
// With at most four labels of at most 63 characters the label rule already keeps
// a valid name well under it, so this check never decides a valid name. It is
// kept anyway, and checked first, because it is the bound every DNS tool will
// apply, and a grammar that says "valid" to something those tools refuse is
// lying about what the name can be used for. Checking it before splitting also
// bounds the work done on hostile input.
const maxTerraNameLength = 253

// Terra name grammar failures. They are separate because the fix differs: a bad
// label is one part to rename, a bad name is the wrong shape altogether.
var (
	// ErrTerraNameInvalid is a reference that is not a well-formed terra name.
	ErrTerraNameInvalid = errors.New("terra name is invalid")
	// ErrDNSLabelInvalid is one dot-separated part that DNS would not accept.
	ErrDNSLabelInvalid = errors.New("dns label is invalid")
)

// dnsLabelPattern is the LDH rule (RFC 1035 §2.3.1 as relaxed by RFC 1123): one
// to 63 lowercase letters, digits and hyphens, not starting or ending with a
// hyphen. Lowercase only, because the grammar compares names byte for byte after
// normalisation, and a pattern that also accepted uppercase would let two
// spellings of one name both be "valid" and then disagree in a map key.
var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// idnaLabelPrefix opens a punycode-encoded internationalised label.
const idnaLabelPrefix = "xn--"

// TerraName is a parsed terra name.
//
// Tree is optional because most people have one Tree, and making them spell it
// in every name would put a word in front of them that carries no information.
// When it is empty the name has three labels; when it is set, four. There is no
// other shape: deeper names would invite readings the index cannot answer, and
// shallower ones could not say whose node it is.
type TerraName struct {
	// Label names the node within its Tree.
	Label string `json:"label"`
	// Tree names one of the handle's Trees, empty for the default one.
	Tree string `json:"tree,omitempty"`
	// Handle is the Terra ID account that owns the Tree. Its extra policy —
	// length, reserved words — is Terra ID's (P6), not this grammar's.
	Handle string `json:"handle"`
}

// ParseTerraName reads a typed reference as a terra name.
//
// It normalises first — surrounding whitespace, ASCII case, one trailing dot —
// because each of those is something a person or a tool adds without meaning
// to, and refusing `Desk.Marui.TERRA.` would only teach people that the grammar
// is fussy, not that the name was wrong. It normalises only that much: anything
// else that differs is a different name or no name, and is refused rather than
// repaired, because a repair that guesses is how a reference ends up naming a
// node nobody meant.
func ParseTerraName(ref string) (TerraName, error) {
	name := normalizeTerraNameRef(ref)
	if name == "" {
		return TerraName{}, fmt.Errorf("%w: terra name must not be empty", ErrTerraNameInvalid)
	}
	if len(name) > maxTerraNameLength {
		return TerraName{}, fmt.Errorf("%w: %d characters is longer than %d",
			ErrTerraNameInvalid, len(name), maxTerraNameLength)
	}
	if !strings.HasSuffix(name, terraNameSuffix) {
		return TerraName{}, fmt.Errorf("%w: %q does not end in %q", ErrTerraNameInvalid, name, terraNameSuffix)
	}
	labels := strings.Split(name, ".")
	if len(labels) != 3 && len(labels) != 4 {
		return TerraName{}, fmt.Errorf("%w: %q has %d labels, want <label>.<handle>.%s or <label>.<tree>.<handle>.%s",
			ErrTerraNameInvalid, name, len(labels), TerraNameTLD, TerraNameTLD)
	}
	for _, label := range labels[:len(labels)-1] {
		if err := ValidateDNSLabel(label); err != nil {
			return TerraName{}, fmt.Errorf("%w: %q: %w", ErrTerraNameInvalid, name, err)
		}
	}
	parsed := TerraName{Label: labels[0], Handle: labels[len(labels)-2]}
	if len(labels) == 4 {
		parsed.Tree = labels[1]
	}
	return parsed, nil
}

// String is the canonical form: lowercase, no trailing dot, three labels when
// Tree is empty. Parsing it gives back the same TerraName, which is what lets
// the string be stored, logged and compared instead of the struct.
func (n TerraName) String() string {
	return n.Label + "." + n.Zone()
}

// Zone is the name without its first label: the Tree the node lives in, as a
// name. `rdk.lab.marui.terra` is in `lab.marui.terra`; `desk.marui.terra` is in
// `marui.terra`. It is the unit a certificate or a resolver is set up for, so
// the parts that serve a whole Tree can ask for it without re-deriving it.
func (n TerraName) Zone() string {
	if n.Tree == "" {
		return n.Handle + terraNameSuffix
	}
	return n.Tree + "." + n.Handle + terraNameSuffix
}

// ValidateDNSLabel checks one label exactly as given.
//
// It does not normalise. The caller decides when a value is typed input that
// may be cleaned (NormalizeDNSLabel) and when it is a stored value that must
// already be canonical; a validator that quietly lowercased would hide the
// second kind of bug.
//
// Labels starting with `xn--` are refused: they are punycode for an
// internationalised name, and accepting them before IDN is deliberately opened
// would let a name that renders as something else entirely — a homograph — in
// through the back door.
func ValidateDNSLabel(label string) error {
	if label == "" {
		return fmt.Errorf("%w: label must not be empty", ErrDNSLabelInvalid)
	}
	if !dnsLabelPattern.MatchString(label) {
		return fmt.Errorf("%w: %q must be 1-63 of a-z, 0-9 and '-', not starting or ending with '-'",
			ErrDNSLabelInvalid, label)
	}
	if strings.HasPrefix(label, idnaLabelPrefix) {
		return fmt.Errorf("%w: %q starts with %q; internationalised labels are not accepted yet",
			ErrDNSLabelInvalid, label, idnaLabelPrefix)
	}
	return nil
}

// NormalizeDNSLabel is the cleaning a typed label gets before validation:
// surrounding whitespace dropped and letters lowercased. It does not validate.
//
// Lowercasing is ASCII only. strings.ToLower would also fold some non-ASCII
// letters onto ASCII ones (U+212A KELVIN SIGN becomes "k"), turning a label that
// should be refused into one that passes and looks identical to a real name.
func NormalizeDNSLabel(label string) string {
	return asciiLower(strings.TrimSpace(label))
}

// normalizeTerraNameRef applies the grammar's normalisation to a whole name.
func normalizeTerraNameRef(ref string) string {
	return strings.TrimSuffix(NormalizeDNSLabel(ref), ".")
}

// HasTerraNameSuffix reports whether ref is shaped like a terra name — ends in
// the TLD, ignoring case, surrounding space and one trailing dot. It is the
// shape test ClassifyNodeRef and ValidateDisplayName share, so the two can never
// disagree about where terra names begin.
func HasTerraNameSuffix(ref string) bool {
	return strings.HasSuffix(normalizeTerraNameRef(ref), terraNameSuffix)
}

// asciiLower lowercases A-Z and leaves every other byte alone.
func asciiLower(value string) string {
	for i := 0; i < len(value); i++ {
		if c := value[i]; 'A' <= c && c <= 'Z' {
			buf := []byte(value)
			for j := i; j < len(buf); j++ {
				if 'A' <= buf[j] && buf[j] <= 'Z' {
					buf[j] += 'a' - 'A'
				}
			}
			return string(buf)
		}
	}
	return value
}
