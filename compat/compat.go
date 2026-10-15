// Package compat parses and evaluates the manifest `compatibility` block:
//
//	{"compatibility": {"core": ">=0.5.0 <1.0.0", "wireContract": 1}}
//
// It is stdlib-only and self-contained in this one file so the rules can be
// changed in one place. The manifest schema field itself is owned by the
// Terra schema; this package only defines how its value is read and judged.
//
// Not to be confused with pkgformat.Compatibility, which shares the manifest
// key but holds different fields (products, hostRoles, keeper, os) describing
// what a package targets. This package reads core and wireContract, the part a
// host (or a market server, for format checks) judges against its own version.
//
// Semver range grammar (a deliberately small subset):
//
//	range      = set { "||" set }
//	set        = comparator { space comparator }     (all must hold)
//	comparator = [ ">=" | ">" | "<=" | "<" | "=" ] version
//	version    = MAJOR.MINOR.PATCH [ "-" prerelease ] [ "+" build ]
//
// Prerelease policy (same idea as npm): a prerelease version such as
// 0.6.0-rc.1 satisfies a set only if some comparator in that set names a
// prerelease of the same MAJOR.MINOR.PATCH. Otherwise prereleases never match,
// even when they sort inside the numeric interval. Build metadata is ignored in
// comparisons. Precedence follows semver 2.0.0 section 11.
package compat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ContractVersion is the version of the module-host wire contract this SDK
// implements (environment variables, header, control paths and their JSON).
// Only ever incremented, and only for an addition or change the host must know
// about. A module that declares no wireContract is treated as version 1.
const ContractVersion = 1

// Compatibility is the parsed `compatibility` manifest block.
type Compatibility struct {
	// Core is a semver range over the Terra core version. Empty = any.
	Core string `json:"core,omitempty"`
	// WireContract is the lowest wire contract version the module needs.
	// 0 (absent) means 1.
	WireContract int `json:"wireContract,omitempty"`
}

// Parse reads the JSON value of the `compatibility` field and validates it.
// Empty input yields the zero Compatibility (compatible with everything).
func Parse(raw []byte) (Compatibility, error) {
	var c Compatibility
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return c, nil
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return Compatibility{}, fmt.Errorf("compatibility: %w", err)
	}
	return c, c.Validate()
}

// Validate checks the range syntax and the wire contract number.
func (c Compatibility) Validate() error {
	if c.WireContract < 0 {
		return errors.New("compatibility.wireContract must be a positive integer")
	}
	if strings.TrimSpace(c.Core) != "" {
		if _, err := ParseRange(c.Core); err != nil {
			return fmt.Errorf("compatibility.core: %w", err)
		}
	}
	return nil
}

// Check judges a host: coreVersion is the Terra core version, hostContract the
// wire contract version the host implements (use ContractVersion for the SDK's
// own). It returns nil when the module may run, else an error saying why.
func (c Compatibility) Check(coreVersion string, hostContract int) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Core) != "" {
		r, _ := ParseRange(c.Core)
		v, err := ParseVersion(coreVersion)
		if err != nil {
			return fmt.Errorf("core version %q: %w", coreVersion, err)
		}
		if !r.Contains(v) {
			return fmt.Errorf("core %s is outside the declared range %q", coreVersion, c.Core)
		}
	}
	need := c.WireContract
	if need == 0 {
		need = 1
	}
	if hostContract < need {
		return fmt.Errorf("module needs wire contract %d, host provides %d", need, hostContract)
	}
	return nil
}

// Version is a parsed semantic version.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 []string // dot-separated prerelease identifiers
}

// ParseVersion parses MAJOR.MINOR.PATCH[-pre][+build]. A leading "v" is
// accepted. Build metadata is dropped.
func ParseVersion(s string) (Version, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var pre string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, pre = s[:i], s[i+1:]
		if pre == "" {
			return Version{}, errors.New("empty prerelease")
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", s)
	}
	var n [3]uint64
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return Version{}, fmt.Errorf("invalid numeric part %q", p)
		}
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("invalid numeric part %q", p)
		}
		n[i] = v
	}
	v := Version{Major: n[0], Minor: n[1], Patch: n[2]}
	if pre != "" {
		v.Pre = strings.Split(pre, ".")
		for _, id := range v.Pre {
			if id == "" {
				return Version{}, errors.New("empty prerelease identifier")
			}
		}
	}
	return v, nil
}

// Compare returns -1, 0, 1 by semver precedence.
func (a Version) Compare(b Version) int {
	for _, p := range [][2]uint64{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.Pre) == 0 && len(b.Pre) == 0:
		return 0
	case len(a.Pre) == 0:
		return 1
	case len(b.Pre) == 0:
		return -1
	}
	for i := 0; i < len(a.Pre) && i < len(b.Pre); i++ {
		if c := comparePre(a.Pre[i], b.Pre[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.Pre) < len(b.Pre):
		return -1
	case len(a.Pre) > len(b.Pre):
		return 1
	}
	return 0
}

func comparePre(x, y string) int {
	xn, xerr := strconv.ParseUint(x, 10, 64)
	yn, yerr := strconv.ParseUint(y, 10, 64)
	switch {
	case xerr == nil && yerr == nil:
		if xn < yn {
			return -1
		} else if xn > yn {
			return 1
		}
		return 0
	case xerr == nil:
		return -1
	case yerr == nil:
		return 1
	}
	return strings.Compare(x, y)
}

type comparator struct {
	op string
	v  Version
}

// Range is a parsed semver range (OR of AND-sets).
type Range struct {
	sets [][]comparator
}

// ParseRange parses a range such as ">=0.5.0 <1.0.0" or "<0.5.0 || >=0.7.0".
func ParseRange(s string) (Range, error) {
	var r Range
	for _, part := range strings.Split(s, "||") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			return Range{}, errors.New("empty comparator set")
		}
		var set []comparator
		for _, f := range fields {
			op := "="
			for _, cand := range []string{">=", "<=", ">", "<", "="} {
				if strings.HasPrefix(f, cand) {
					op, f = cand, strings.TrimPrefix(f, cand)
					break
				}
			}
			v, err := ParseVersion(f)
			if err != nil {
				return Range{}, fmt.Errorf("comparator %q: %w", op+f, err)
			}
			set = append(set, comparator{op, v})
		}
		r.sets = append(r.sets, set)
	}
	return r, nil
}

// Contains reports whether v satisfies the range (prerelease policy above).
func (r Range) Contains(v Version) bool {
	for _, set := range r.sets {
		if setHolds(set, v) {
			return true
		}
	}
	return false
}

func setHolds(set []comparator, v Version) bool {
	if len(v.Pre) > 0 {
		ok := false
		for _, c := range set {
			if len(c.v.Pre) > 0 && c.v.Major == v.Major && c.v.Minor == v.Minor && c.v.Patch == v.Patch {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	for _, c := range set {
		d := v.Compare(c.v)
		var pass bool
		switch c.op {
		case ">=":
			pass = d >= 0
		case ">":
			pass = d > 0
		case "<=":
			pass = d <= 0
		case "<":
			pass = d < 0
		default:
			pass = d == 0
		}
		if !pass {
			return false
		}
	}
	return true
}
