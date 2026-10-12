package pkgformat

import (
	"encoding/json"
	"errors"
	"fmt"
	sdkmodulert "github.com/StellaxiaLab/terra-sdk/modulert"
	"regexp"
	"slices"
	"strings"
)

// ManifestSchemaVersion is the canonical Manifest version this package owns.
const ManifestSchemaVersion = 2

// ManifestFileName is the package-root manifest file.
const ManifestFileName = "module.json"

var moduleIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,127}$`)

var sha256HexPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const KindApplication = "application"

const KindRuntime = "runtime"

const KindScene = "scene"

const KindService = "service"

const KindAdapter = "adapter"

// KindExtension is a Scene Extension package: a sandboxed bundle the Player
// loads into an isolated realm. Like a scene it is data, not a process —
// its implementation is named by contributions.gui.extensions[].entry.
const KindExtension = "extension"

// manifestKinds lists every kind Validate accepts, and is the one place the
// list is written.
//
// It exists because the list was written twice and the second copy did not
// grow. schema_mirror_test.go held its own set to compare the published schema
// against; when KindExtension arrived, neither the schema's enum nor that set
// was updated, so the guard compared two stale lists, agreed with itself, and
// a shipped module (io.terra.ext.metrics) became one the distributed schema
// rejects. A drift test that carries a copy of what it is checking can only
// catch the drift it does not share.
var manifestKinds = []string{
	KindApplication,
	KindRuntime,
	KindScene,
	KindService,
	KindAdapter,
	KindExtension,
}

// ManifestKinds returns the kinds a v2 manifest may declare, in schema order.
func ManifestKinds() []string { return append([]string(nil), manifestKinds...) }

// Manifest is the canonical Manifest v2 model (schemaVersion 2). Fields
// whose schema is owned by the host (contributions, signature) are held
// as flexible values here; this package fixes identity, entrypoints, provides,
// permissions, readiness, update and lifecycle.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Version       string            `json:"version"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name,omitempty"`
	Description   string            `json:"description,omitempty"`
	Compatibility *Compatibility    `json:"compatibility,omitempty"`
	Dependencies  []Dependency      `json:"dependencies,omitempty"`
	Entrypoints   Entrypoints       `json:"entrypoints,omitempty"`
	Integrity     *Integrity        `json:"integrity,omitempty"`
	Provides      *Provides         `json:"provides,omitempty"`
	Contracts     map[string]string `json:"contracts,omitempty"`
	Contributions map[string]any    `json:"contributions,omitempty"`
	Permissions   *Permissions      `json:"permissions,omitempty"`
	Readiness     *Readiness        `json:"readiness,omitempty"`
	Update        *UpdatePolicy     `json:"update,omitempty"`
	// Configuration declares what an operator can set for this module
	// (the module configuration schema). The values themselves belong to the host.
	Configuration *Configuration `json:"configuration,omitempty"`
	Signature     map[string]any `json:"signature,omitempty"`
	// Legacy preserves v1-only fields so v1→v2 normalization loses nothing.
	Legacy *LegacyV1 `json:"legacy,omitempty"`
}

type Compatibility struct {
	Products  []string `json:"products,omitempty"`
	HostRoles []string `json:"hostRoles,omitempty"`
	Keeper    string   `json:"keeper,omitempty"`
	OS        []string `json:"os,omitempty"`
}

type Dependency struct {
	ID           string `json:"id,omitempty"`
	Capability   string `json:"capability,omitempty"`
	VersionRange string `json:"versionRange,omitempty"`
}

// Entrypoints groups per-kind entry declarations. Process/Worker/Library/
// Runtime map a normalized target (e.g. "linux-amd64") to a package-relative
// path; Scene names the scene entry.
type Entrypoints struct {
	Process map[string]string `json:"process,omitempty"`
	Worker  map[string]string `json:"worker,omitempty"`
	Library map[string]string `json:"library,omitempty"`
	Runtime map[string]string `json:"runtime,omitempty"`
	Scene   string            `json:"scene,omitempty"`
}

func (e Entrypoints) isEmpty() bool {
	return len(e.Process) == 0 && len(e.Worker) == 0 && len(e.Library) == 0 && len(e.Runtime) == 0 && e.Scene == ""
}

// Integrity carries digests for package content so a host can verify the
// on-disk files before launch. v1 manifests carried the digest inline on each
// entrypoint (preserved as Legacy.EntrypointHashes); the canonical v2 form is
// this block, stamped by the release build when it packages the module. A
// signed manifest must be signed AFTER stamping — the digests are part of the
// canonical signature payload.
//
// Process covers executables only and is what the host re-checks at launch.
// Parts covers the whole payload — every packaged file, grouped into a
// "common" part and one part per target (distribution D-2) — and is what the
// installer re-checks after unpacking. When both name the same file they must
// agree; Validate rejects the manifest otherwise, so there is never a question
// of which digest the signature covered.
type Integrity struct {
	// Process maps a normalized target (e.g. "linux-amd64") to the lowercase
	// hex SHA-256 of that target's process entrypoint file.
	Process map[string]string `json:"process,omitempty"`
	// Parts partitions the package's files for payload verification and, later,
	// per-target partial transfer. Every packaged file except module.json
	// itself belongs to exactly one part.
	Parts []IntegrityPart `json:"parts,omitempty"`
}

// IntegrityPart is one named group of packaged files: "common" for files every
// installation needs, or a target name (e.g. "linux-amd64") for files only
// that target needs. Files maps package-relative slash paths to lowercase hex
// SHA-256 digests. The manifest itself is never listed — it cannot carry its
// own digest; the signature covers it instead.
type IntegrityPart struct {
	Name string `json:"name"`
	// SHA256, when set, is the part's canonical digest (CanonicalPartDigest
	// over Files). It gives the part a stable content address for partial
	// transfer without a second serialization format.
	SHA256 string            `json:"sha256,omitempty"`
	Files  map[string]string `json:"files"`
}

type Provides struct {
	Capabilities []Capability `json:"capabilities,omitempty"`
}

type Capability struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	// The fields below are the operation's execution facts, filled only where a
	// node reports its catalog upward (ModuleCatalogEntry.Operations,
	// CoreOperationCatalog) so a remote caller can tell whether it may call and
	// whether a confirmation is needed . A manifest never
	// sets them; its contract is the source.
	Permissions      []string `json:"permissions,omitempty"`
	Risk             string   `json:"risk,omitempty"`
	ConfirmationMode string   `json:"confirmationMode,omitempty"`
	OutputMode       string   `json:"outputMode,omitempty"`
}

type Permissions struct {
	CoreOperations []Dependency `json:"coreOperations,omitempty"`
	Storage        []string     `json:"storage,omitempty"`
	Network        []string     `json:"network,omitempty"`
	Secret         []string     `json:"secret,omitempty"`
	Native         []string     `json:"native,omitempty"`
}

type UpdatePolicy struct {
	Strategy       string `json:"strategy,omitempty"`
	DrainTimeoutMS int    `json:"drainTimeoutMs,omitempty"`
	Rollback       bool   `json:"rollback,omitempty"`
}

// declaresNonExecutableImplementation reports whether this manifest names its
// implementation somewhere other than an entrypoint. It is deliberately
// per-kind rather than "any contribution counts": a service module that forgot
// its entrypoint but contributes a GUI app is still broken, and inferring
// otherwise would turn a caught mistake into a module that installs and never
// answers.
func (m *Manifest) declaresNonExecutableImplementation() bool {
	switch m.Kind {
	case KindScene:
		// The scene entry lives in contributions (or entrypoints.scene).
		return true
	case KindAdapter:
		return ManifestWrapsExternalService(*m)
	case KindExtension:
		// A scene gets an unconditional pass because entrypoints.scene is an
		// alternative spelling. An extension has no such fallback, so the
		// contribution has to actually be there — otherwise the package
		// installs and contributes nothing, which is the shape this check
		// exists to catch.
		return manifestDeclaresGuiExtensions(*m)
	default:
		return false
	}
}

// manifestDeclaresGuiExtensions reports whether contributions.gui.extensions
// holds at least one entry. Contributions are untyped here on purpose — this
// package owns the canonical fields, and the Gateway owns what a GUI
// contribution means — so this reads the shape without modelling it.
func manifestDeclaresGuiExtensions(m Manifest) bool {
	gui, ok := m.Contributions["gui"].(map[string]any)
	if !ok {
		return false
	}
	extensions, ok := gui["extensions"].([]any)
	return ok && len(extensions) > 0
}

// ParseManifest decodes and validates a canonical Manifest v2 document.
func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode module manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate normalizes and checks the canonical fields this package owns.
func (m *Manifest) Validate() error {
	if m == nil {
		return errors.New("module manifest is required")
	}
	m.ID = strings.TrimSpace(m.ID)
	m.Version = strings.TrimSpace(m.Version)
	m.Kind = strings.ToLower(strings.TrimSpace(m.Kind))
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported module manifest schemaVersion %d (expected %d)", m.SchemaVersion, ManifestSchemaVersion)
	}
	if !moduleIDPattern.MatchString(m.ID) {
		return fmt.Errorf("invalid module id %q", m.ID)
	}
	if m.Version == "" {
		return errors.New("module version is required")
	}
	if !slices.Contains(manifestKinds, m.Kind) {
		return fmt.Errorf("unsupported module kind %q", m.Kind)
	}
	// Every module must declare where its implementation is, but not every
	// implementation is an executable in this package: a scene is data the
	// Player loads, and an adapter may wrap a service that already runs
	// elsewhere. Both say so through contributions, so the requirement is an
	// entrypoint OR a contribution that stands in for one — spelling it as
	// "entrypoint required" rejected adapter packages the Gateway serves.
	if m.Entrypoints.isEmpty() && !m.declaresNonExecutableImplementation() {
		return fmt.Errorf("module kind %q requires at least one entrypoint, or a contribution naming its implementation", m.Kind)
	}
	if m.Integrity != nil {
		for target, digest := range m.Integrity.Process {
			digest = strings.ToLower(strings.TrimSpace(digest))
			if !sha256HexPattern.MatchString(digest) {
				return fmt.Errorf("integrity.process[%q] is not a lowercase hex sha256", target)
			}
			if _, declared := m.Entrypoints.Process[target]; !declared {
				return fmt.Errorf("integrity.process[%q] has no matching process entrypoint", target)
			}
			m.Integrity.Process[target] = digest
		}
		if err := m.validateIntegrityParts(); err != nil {
			return err
		}
	}
	if m.Configuration != nil {
		if _, err := ParseConfigSchema(m.Configuration.Schema); err != nil {
			return err
		}
	}
	return nil
}

// Moved to the public SDK (github.com/StellaxiaLab/terra-sdk/modulert). The names below stay importable from this
// package as aliases so callers do not change (design D-3).
type Readiness = sdkmodulert.Readiness

// ValidModuleID reports whether id is a well-formed module id: lower-case
// alphanumerics, '.', '_' and '-', starting with an alphanumeric, 3-128 bytes.
// Manifest.Validate applies the same rule to Manifest.ID.
func ValidModuleID(id string) bool { return moduleIDPattern.MatchString(id) }
