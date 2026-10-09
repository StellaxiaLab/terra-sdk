package modulert

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SharedRootsPermission is the permissions.storage value a module declares to
// be told about the node's shared folders. It sits beside DataDirName, and the
// two are not alike: module-data is the module's own corner of the product data
// dir, while this names directories that belong to the person using the
// machine.
//
// That asymmetry is why the grant is gated on trust tier as well as on the
// declaration (see the host's shared-roots grant). A module's own data is its
// own; a user's shared folder is not.
const SharedRootsPermission = "shared-roots"

// SharedRootsEnv carries the granted roots to a module process as JSON — an
// array of SharedRoot. A module that declared the permission and was granted it
// reads this; anything else does not see the variable at all.
const SharedRootsEnv = "TERRA_MODULE_SHARED_ROOTS"

// SharedRoot is one shared folder: the name it answers to and where it is.
//
// The name exists because the path is the host's business and the name is the
// contract. Without it a module has to work out "the first one" from position,
// which is what the daemon's own storage manager does today (share-0, share-1…)
// and is exactly the guess this type removes.
type SharedRoot struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// PositionalSharedRootName is the name a root gets when the configuration did
// not give it one. It matches what the daemon's storage manager has always
// called them, so an existing deployment keeps the names it already had.
func PositionalSharedRootName(index int) string {
	return fmt.Sprintf("share-%d", index)
}

// UnmarshalJSON accepts both shapes a configuration may carry:
//
//	"~/TerraShare"                              → an unnamed root
//	{"name": "media", "path": "/srv/media"}     → a named one
//
// The string form is what every existing config file holds, and rewriting those
// files was never worth it — the shapes cost one decoder between them.
// An unnamed root is left unnamed here; NameSharedRoots fills it in by position
// once the whole list is known, which is the only place the index exists.
func (r *SharedRoot) UnmarshalJSON(data []byte) error {
	var path string
	if err := json.Unmarshal(data, &path); err == nil {
		r.Name, r.Path = "", strings.TrimSpace(path)
		return nil
	}
	// The alias sheds this method so the object form decodes without recursing.
	type plain SharedRoot
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("a shared root must be a path string or {name, path}: %w", err)
	}
	r.Name = strings.TrimSpace(decoded.Name)
	r.Path = strings.TrimSpace(decoded.Path)
	return nil
}

// MarshalJSON writes back the shape the root came in as: a bare path when it
// never had a name of its own. A daemon that saves its configuration should not
// rewrite a file the operator wrote by hand.
func (r SharedRoot) MarshalJSON() ([]byte, error) {
	if r.Name == "" {
		return json.Marshal(r.Path)
	}
	type plain SharedRoot
	return json.Marshal(plain(r))
}

// NameSharedRoots fills in the positional name of every root that has none and
// returns the list. Roots with an empty path are dropped: a blank entry in a
// config file is a typo, and carrying it forward would hand a module a root
// that resolves to the process working directory.
//
// A name given twice is left as it is rather than renamed. Detecting it here
// would report the collision from a place with nothing useful to say about it;
// the storage layer that opens the roots is where a duplicate actually bites.
func NameSharedRoots(roots []SharedRoot) []SharedRoot {
	named := make([]SharedRoot, 0, len(roots))
	for index, root := range roots {
		if strings.TrimSpace(root.Path) == "" {
			continue
		}
		if root.Name == "" {
			root.Name = PositionalSharedRootName(index)
		}
		named = append(named, root)
	}
	return named
}

// SharedRootPaths is the plain path list, for the callers that predate names
// and only ever wanted somewhere to look.
func SharedRootPaths(roots []SharedRoot) []string {
	paths := make([]string, 0, len(roots))
	for _, root := range roots {
		paths = append(paths, root.Path)
	}
	return paths
}
