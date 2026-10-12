package pkgformat

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const PermissionKindCoreOperation = "core.operation"

const PermissionKindStorage = "storage"

const PermissionKindNetwork = "network"

const PermissionKindSecret = "secret"

const PermissionKindNative = "native"

// ErrInvalidPermission is returned for a permissions block that cannot be
// flattened into canonical strings: an empty value, or a core operation id
// that contains ':'.
var ErrInvalidPermission = errors.New("invalid permission declaration")

// CanonicalPermissions returns the canonical permission set of a manifest
// (the canonical set): "kind:value" strings, sorted, without duplicates. A
// manifest without a permissions block yields an empty, non-nil slice.
func CanonicalPermissions(m Manifest) ([]string, error) {
	return CanonicalPermissionsOf(m.Permissions)
}

// CanonicalPermissionsOf is CanonicalPermissions over a bare permissions block.
func CanonicalPermissionsOf(p *Permissions) ([]string, error) {
	out := []string{}
	if p == nil {
		return out, nil
	}
	for i, d := range p.CoreOperations {
		name := strings.TrimSpace(d.ID)
		if name == "" {
			name = strings.TrimSpace(d.Capability)
		}
		if name == "" {
			return nil, fmt.Errorf("%w: permissions.coreOperations[%d] names no id or capability", ErrInvalidPermission, i)
		}
		if strings.Contains(name, ":") {
			return nil, fmt.Errorf("%w: core operation id %q contains ':'", ErrInvalidPermission, name)
		}
		out = append(out, PermissionKindCoreOperation+":"+name)
	}
	for _, group := range []struct {
		kind   string
		values []string
	}{
		{PermissionKindStorage, p.Storage},
		{PermissionKindNetwork, p.Network},
		{PermissionKindSecret, p.Secret},
		{PermissionKindNative, p.Native},
	} {
		for i, v := range group.values {
			v = strings.TrimSpace(v)
			if v == "" {
				return nil, fmt.Errorf("%w: permissions.%s[%d] is empty", ErrInvalidPermission, group.kind, i)
			}
			out = append(out, group.kind+":"+v)
		}
	}
	return sortedUnique(out), nil
}

// NormalizePermission reads one stored permission string in canonical form.
// A value without ':' is the legacy form — a bare core operation id, which is
// how consent records and tree envelopes stored grants before the canonical form existed — and
// reads as "core.operation:<id>". Surrounding space is trimmed; an empty value
// stays empty.
func NormalizePermission(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, ":") {
		return s
	}
	return PermissionKindCoreOperation + ":" + s
}

// NormalizePermissions normalizes every value (NormalizePermission), drops
// empties, and returns the result sorted and de-duplicated. A nil input yields
// nil so "no record" stays distinguishable from "an empty record".
func NormalizePermissions(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if n := NormalizePermission(v); n != "" {
			out = append(out, n)
		}
	}
	return sortedUnique(out)
}

// PermissionsNotIn returns the canonical members of want that are absent from
// have (both read through NormalizePermission), sorted. Nil when want is
// covered.
func PermissionsNotIn(want, have []string) []string {
	set := make(map[string]bool, len(have))
	for _, h := range have {
		set[NormalizePermission(h)] = true
	}
	var missing []string
	for _, w := range NormalizePermissions(want) {
		if !set[w] {
			missing = append(missing, w)
		}
	}
	return missing
}

// CoreOperationPermission is the canonical permission string for a core
// operation id.
func CoreOperationPermission(operationID string) string {
	return PermissionKindCoreOperation + ":" + strings.TrimSpace(operationID)
}

func sortedUnique(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, v := range in {
		if i > 0 && v == in[i-1] {
			continue
		}
		out = append(out, v)
	}
	return out
}
