package pkgformat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ManifestV1 is the legacy Daemon module manifest (schema_version 1, snake_case).
// It is decoded here so the common package can normalize existing v1 modules to
// v2 while v1 manifests are still accepted.
type ManifestV1 struct {
	SchemaVersion int                     `json:"schema_version"`
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	Version       string                  `json:"version"`
	Description   string                  `json:"description,omitempty"`
	Kind          string                  `json:"kind"`
	Enabled       bool                    `json:"enabled"`
	AutoStart     bool                    `json:"auto_start"`
	Entrypoints   map[string]EntrypointV1 `json:"entrypoints"`
	Args          []string                `json:"args,omitempty"`
	Environment   map[string]string       `json:"environment,omitempty"`
	Restart       RestartV1               `json:"restart"`
	Capabilities  CapabilitiesV1          `json:"capabilities,omitempty"`
	Permissions   []string                `json:"permissions,omitempty"`
}

type EntrypointV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
}

type RestartV1 struct {
	Policy      string `json:"policy"`
	MaxRestarts int    `json:"max_restarts,omitempty"`
	BackoffMS   int    `json:"backoff_ms,omitempty"`
}

type CapabilitiesV1 struct {
	Resources []string `json:"resources,omitempty"`
	Actions   []string `json:"actions,omitempty"`
	Signals   []string `json:"signals,omitempty"`
}

// LegacyV1 preserves v1-only fields on a normalized v2 manifest so nothing is
// dropped in transition. Fields with a clean v2 home (id, version, kind,
// process entrypoints) are promoted; the rest live here.
type LegacyV1 struct {
	Enabled          bool              `json:"enabled,omitempty"`
	AutoStart        bool              `json:"autoStart,omitempty"`
	Args             []string          `json:"args,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Restart          *LegacyRestart    `json:"restart,omitempty"`
	Capabilities     *CapabilitiesV1   `json:"capabilities,omitempty"`
	Permissions      []string          `json:"permissions,omitempty"`
	EntrypointHashes map[string]string `json:"entrypointHashes,omitempty"`
}

type LegacyRestart struct {
	Policy      string `json:"policy,omitempty"`
	MaxRestarts int    `json:"maxRestarts,omitempty"`
	BackoffMS   int    `json:"backoffMs,omitempty"`
}

// DecodeV1 decodes a legacy v1 manifest document without validating v2 rules.
func DecodeV1(data []byte) (ManifestV1, error) {
	var manifest ManifestV1
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ManifestV1{}, fmt.Errorf("decode v1 module manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return ManifestV1{}, fmt.Errorf("not a v1 module manifest (schema_version %d)", manifest.SchemaVersion)
	}
	return manifest, nil
}

// NormalizeV1 mechanically maps a v1 manifest to the canonical v2 model,
// preserving every v1 field. v1 modules are process modules, so their
// entrypoints become entrypoints.process; entrypoint hashes and all v1-only
// fields are carried under legacy. v1 permissions and capabilities are also
// lifted into their structured v2 homes (Permissions, provides.capabilities)
// while the verbatim v1 lists stay under legacy. The result passes Validate for
// a well-formed v1 input.
func NormalizeV1(v1 ManifestV1) Manifest {
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		ID:            strings.TrimSpace(v1.ID),
		Version:       strings.TrimSpace(v1.Version),
		Kind:          strings.ToLower(strings.TrimSpace(v1.Kind)),
		Name:          strings.TrimSpace(v1.Name),
		Description:   strings.TrimSpace(v1.Description),
	}

	legacy := &LegacyV1{
		Enabled:     v1.Enabled,
		AutoStart:   v1.AutoStart,
		Args:        append([]string(nil), v1.Args...),
		Environment: copyStringMap(v1.Environment),
		Permissions: append([]string(nil), v1.Permissions...),
	}

	if len(v1.Entrypoints) > 0 {
		process := make(map[string]string, len(v1.Entrypoints))
		hashes := map[string]string{}
		for target, entrypoint := range v1.Entrypoints {
			target = strings.TrimSpace(target)
			process[target] = strings.TrimSpace(entrypoint.Path)
			if hash := strings.ToLower(strings.TrimSpace(entrypoint.SHA256)); hash != "" {
				hashes[target] = hash
			}
		}
		manifest.Entrypoints.Process = process
		if len(hashes) > 0 {
			legacy.EntrypointHashes = hashes
		}
	}

	if v1.Restart != (RestartV1{}) {
		legacy.Restart = &LegacyRestart{
			Policy:      strings.ToLower(strings.TrimSpace(v1.Restart.Policy)),
			MaxRestarts: v1.Restart.MaxRestarts,
			BackoffMS:   v1.Restart.BackoffMS,
		}
	}
	if capabilitiesV1NonEmpty(v1.Capabilities) {
		capabilities := v1.Capabilities
		legacy.Capabilities = &capabilities
	}

	manifest.Permissions = mapV1Permissions(v1.Permissions)
	manifest.Provides = mapV1Capabilities(v1.Capabilities, manifest.Version)
	manifest.Legacy = legacy
	return manifest
}

// mapV1Permissions lifts a flat v1 permission list into the structured v2
// Permissions model. A v1 permission is a dotted token (e.g.
// "daemon.svi.resource.read", "os.virtual-device.use"); its first segment routes
// it to a resource category, and anything unrecognized is treated as a Core
// operation permission (Dependency without a versionRange, which v1 lacks).
// Returns nil when no permission maps, so the field stays omitted.
func mapV1Permissions(perms []string) *Permissions {
	if len(perms) == 0 {
		return nil
	}
	out := &Permissions{}
	for _, raw := range perms {
		perm := strings.TrimSpace(raw)
		if perm == "" {
			continue
		}
		switch permissionCategory(perm) {
		case "native":
			out.Native = append(out.Native, perm)
		case "network":
			out.Network = append(out.Network, perm)
		case "storage":
			out.Storage = append(out.Storage, perm)
		case "secret":
			out.Secret = append(out.Secret, perm)
		default:
			out.CoreOperations = append(out.CoreOperations, Dependency{ID: perm})
		}
	}
	if len(out.CoreOperations) == 0 && len(out.Storage) == 0 && len(out.Network) == 0 && len(out.Secret) == 0 && len(out.Native) == 0 {
		return nil
	}
	return out
}

// permissionCategory routes a v1 permission token to a v2 Permissions category
// by its first dot segment. Unrecognized prefixes fall to "core".
func permissionCategory(perm string) string {
	prefix := perm
	if i := strings.IndexByte(perm, '.'); i >= 0 {
		prefix = perm[:i]
	}
	switch strings.ToLower(prefix) {
	case "os", "native":
		return "native"
	case "net", "network":
		return "network"
	case "storage":
		return "storage"
	case "secret":
		return "secret"
	default:
		return "core"
	}
}

// mapV1Capabilities lifts a v1 capability surface to v2 provides.capabilities.
// Resources and actions are state others may query or operations others may
// invoke, so each becomes a provided capability {id, version} (version = the
// module version) that the dependency resolver can match. Signals are
// subscribable event contracts , not resolver capabilities, so
// they are preserved under legacy only and not lifted here. Duplicate ids are
// collapsed. Returns nil when nothing maps.
func mapV1Capabilities(c CapabilitiesV1, version string) *Provides {
	version = strings.TrimSpace(version)
	var capabilities []Capability
	seen := make(map[string]bool)
	for _, id := range append(append([]string(nil), c.Resources...), c.Actions...) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		capabilities = append(capabilities, Capability{ID: id, Version: version})
	}
	if len(capabilities) == 0 {
		return nil
	}
	return &Provides{Capabilities: capabilities}
}

// NormalizeV1Bytes decodes a v1 manifest document and normalizes it to v2.
func NormalizeV1Bytes(data []byte) (Manifest, error) {
	v1, err := DecodeV1(data)
	if err != nil {
		return Manifest{}, err
	}
	return NormalizeV1(v1), nil
}

func capabilitiesV1NonEmpty(c CapabilitiesV1) bool {
	return len(c.Resources) > 0 || len(c.Actions) > 0 || len(c.Signals) > 0
}

func copyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
