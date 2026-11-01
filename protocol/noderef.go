package protocol

// The part of the node-reference rules a hosted-access service needs: what a
// node id looks like and how a reference is classified. The index that resolves
// references against enrolled nodes stays in Terra (terra-protocol/noderef.go);
// it re-exports these names, so the shapes cannot drift between the two.

import (
	"net/netip"
	"regexp"
	"strings"
)

// NodeIDPrefix is the reserved opening of every Master-issued node id.
//
// Reserving it is what lets a reference that starts with it be taken as an id
// without asking anyone — the only reason a reference works when the Master is
// unreachable. The shortcut is sound because Terra refuses display names in
// this shape.
const NodeIDPrefix = "node_"

// NodeRefKind is which of a node's names a reference was read as.
type NodeRefKind string

const (
	// NodeRefUnknown is the zero value: no reference was classified.
	NodeRefUnknown NodeRefKind = ""
	// NodeRefNodeID is the Master-issued identifier.
	NodeRefNodeID NodeRefKind = "node_id"
	// NodeRefClusterAddress is the node's address inside its cluster.
	NodeRefClusterAddress NodeRefKind = "cluster_address"
	// NodeRefDisplayName is the label an operator chose.
	NodeRefDisplayName NodeRefKind = "display_name"
	// NodeRefTerraName is a `<label>[.<tree>].<handle>.terra` name (terraname.go).
	// It is not a fourth name stored on a node: it is a way to reach the node
	// from outside its cluster, and like the others it resolves to a node_id.
	NodeRefTerraName NodeRefKind = "terra_name"
)

// nodeIDPattern is the shape the Master issues: the reserved prefix, then
// characters that survive being a path segment and a file name. Node ids reach
// the filesystem in more than one subsystem, so the character set is the
// intersection of what all of them accept rather than the union.
var nodeIDPattern = regexp.MustCompile(`^node_[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// ValidNodeID reports whether id is a well-formed Master-issued node id.
func ValidNodeID(id string) bool { return nodeIDPattern.MatchString(strings.TrimSpace(id)) }

// ClassifyNodeRef says which kind of name a reference is, by shape alone.
//
// Telling them apart is what lets a reference be understood while the Master
// is unreachable. A reference ending in `.terra` is a terra name even when it
// is malformed, so ParseTerraName can say what is wrong with it instead of it
// missing as a display name. Anything that is none of these is a display name,
// including nonsense, because "not found" is a better answer to a typo than
// "not a valid reference".
func ClassifyNodeRef(ref string) NodeRefKind {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return NodeRefUnknown
	}
	if strings.HasPrefix(ref, NodeIDPrefix) {
		return NodeRefNodeID
	}
	if _, err := netip.ParseAddr(ref); err == nil {
		return NodeRefClusterAddress
	}
	if HasTerraNameSuffix(ref) {
		return NodeRefTerraName
	}
	return NodeRefDisplayName
}
