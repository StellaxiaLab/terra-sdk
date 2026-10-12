package pkgformat

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// craftZip hand-builds an archive that WritePackage would refuse to produce —
// the attacks arrive as bytes, not through our writer.
func craftZip(t *testing.T, build func(*zip.Writer)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crafted.tmod")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(out)
	build(writer)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// hostileArchiveCase is one archive that must be refused, and the substring
// that names why. The table is shared: extraction refuses these, and after
// U-3 so does inspection — see TestInspectRefusesEverythingExtractRefuses.
type hostileArchiveCase struct {
	build   func(*zip.Writer)
	limits  ArchiveLimits
	message string
}

func hostileArchiveCases() map[string]hostileArchiveCase {
	return map[string]hostileArchiveCase{
		"zipSlip": {
			build: func(w *zip.Writer) {
				entry, _ := w.Create("../evil.txt")
				entry.Write([]byte("escape"))
			},
			limits:  DefaultArchiveLimits(),
			message: "clean relative path",
		},
		"absolutePath": {
			build: func(w *zip.Writer) {
				entry, _ := w.Create("/etc/passwd")
				entry.Write([]byte("root"))
			},
			limits:  DefaultArchiveLimits(),
			message: "package-relative",
		},
		"backslashPath": {
			build: func(w *zip.Writer) {
				entry, _ := w.Create(`ui\index.html`)
				entry.Write([]byte("x"))
			},
			limits:  DefaultArchiveLimits(),
			message: "forward slashes",
		},
		"symlinkEntry": {
			build: func(w *zip.Writer) {
				header := &zip.FileHeader{Name: "link"}
				header.SetMode(os.ModeSymlink | 0o777)
				entry, _ := w.CreateHeader(header)
				entry.Write([]byte("/etc/passwd"))
			},
			limits:  DefaultArchiveLimits(),
			message: "symlink",
		},
		"duplicateEntry": {
			build: func(w *zip.Writer) {
				first, _ := w.Create("ui/a")
				first.Write([]byte("one"))
				second, _ := w.Create("ui/a")
				second.Write([]byte("two"))
			},
			limits:  DefaultArchiveLimits(),
			message: "twice",
		},
		"tooManyEntries": {
			build: func(w *zip.Writer) {
				for _, name := range []string{"a", "b", "c"} {
					entry, _ := w.Create(name)
					entry.Write([]byte(name))
				}
			},
			limits:  ArchiveLimits{MaxEntries: 2, MaxTotalBytes: 1 << 20, MaxManifestBytes: 1 << 20, MaxCompressionRatio: 200},
			message: "entries",
		},
		"totalSize": {
			build: func(w *zip.Writer) {
				entry, _ := w.Create("big")
				entry.Write(bytes.Repeat([]byte("x"), 4096))
			},
			limits:  ArchiveLimits{MaxEntries: 100, MaxTotalBytes: 1024, MaxManifestBytes: 1 << 20, MaxCompressionRatio: 200},
			message: "size limit",
		},
	}
}

func TestOpenPackageManifestRequiresManifest(t *testing.T) {
	path := craftZip(t, func(w *zip.Writer) {
		entry, _ := w.Create("ui/index.html")
		entry.Write([]byte("<html>"))
	})
	if _, _, err := OpenPackageManifest(path, DefaultArchiveLimits()); err == nil {
		t.Fatal("package without module.json must be rejected")
	}
}

// designExampleSchema is a representative configuration schema.
const designExampleSchema = `{
  "type": "object",
  "properties": {
    "camera_url": { "type": "string", "format": "uri", "title": "카메라 주소" },
    "fps":        { "type": "integer", "minimum": 1, "maximum": 60, "default": 15 },
    "api_token":  { "type": "string", "x-terra-secret": true }
  },
  "required": ["camera_url"],
  "additionalProperties": false
}`

func decodeSchema(t *testing.T, text string) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustSchema(t *testing.T, text string) *ConfigSchema {
	t.Helper()
	schema, err := ParseConfigSchema(decodeSchema(t, text))
	if err != nil {
		t.Fatalf("ParseConfigSchema: %v", err)
	}
	return schema
}

func TestManifestRefusesConfigurationOutsideSubset(t *testing.T) {
	for name, schema := range map[string]string{
		"missing schema":      `{}`,
		"root not object":     `{"schema":{"type":"string"}}`,
		"unknown keyword":     `{"schema":{"type":"object","properties":{"a":{"type":"string","oneOf":[]}}}}`,
		"$ref":                `{"schema":{"type":"object","$ref":"#/x"}}`,
		"type array":          `{"schema":{"type":"object","properties":{"a":{"type":["string","null"]}}}}`,
		"bad format":          `{"schema":{"type":"object","properties":{"a":{"type":"string","format":"email"}}}}`,
		"keyword wrong type":  `{"schema":{"type":"object","properties":{"a":{"type":"integer","pattern":"x"}}}}`,
		"bad default":         `{"schema":{"type":"object","properties":{"a":{"type":"integer","maximum":3,"default":9}}}}`,
		"required undeclared": `{"schema":{"type":"object","properties":{},"required":["a"]}}`,
		"nested secret":       `{"schema":{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"string","x-terra-secret":true}}}}}}`,
		"bad pattern":         `{"schema":{"type":"object","properties":{"a":{"type":"string","pattern":"("}}}}`,
		"schema additional":   `{"schema":{"type":"object","additionalProperties":{"type":"string"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			document := `{"schemaVersion":2,"id":"io.example.camera","version":"1.0.0","kind":"service",
			  "entrypoints":{"process":{"linux-amd64":"bin/camera"}},"configuration":` + schema + `}`
			if _, err := ParseManifest([]byte(document)); err == nil {
				t.Fatal("ParseManifest accepted a configuration schema outside the subset")
			}
		})
	}
}

func TestConfigSchemaSizeLimit(t *testing.T) {
	properties := map[string]any{}
	for i := 0; len(properties) < 2000; i++ {
		properties[strings.Repeat("k", 20)+string(rune('a'+i%26))+strings.Repeat("x", i/26)] = map[string]any{"type": "string", "description": strings.Repeat("d", 20)}
	}
	if _, err := ParseConfigSchema(map[string]any{"type": "object", "properties": properties}); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("an over-64KiB schema must be refused, got %v", err)
	}
}

func TestConfigEffectiveFillsDefaultsAndValidates(t *testing.T) {
	schema := mustSchema(t, designExampleSchema)
	effective, err := schema.Effective(map[string]any{"camera_url": "rtsp://10.0.0.5/stream"})
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if effective["fps"] != float64(15) {
		t.Fatalf("default not filled: %v", effective)
	}
	if _, err := schema.Effective(map[string]any{}); err == nil {
		t.Fatal("missing required key must be refused")
	}
	_, err = schema.Effective(map[string]any{"camera_url": "not a uri", "fps": 61.5, "extra": true, "api_token": 3.0})
	var invalid *ConfigInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v", err)
	}
	got := map[string]ConfigKeyIssueCode{}
	for _, issue := range invalid.Keys {
		got[issue.Key] = issue.Code
		if strings.Contains(issue.Message, "not a uri") || strings.Contains(issue.Message, "61.5") {
			t.Fatalf("issue message echoes the value: %+v", issue)
		}
	}
	want := map[string]ConfigKeyIssueCode{"camera_url": ConfigIssueValidation, "fps": ConfigIssueType, "extra": ConfigIssueUnknown, "api_token": ConfigIssueType}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

func TestConfigSchemaKeywordCoverage(t *testing.T) {
	schema := mustSchema(t, `{"type":"object","properties":{
	  "mode":  {"type":"string","enum":["a","b"]},
	  "name":  {"type":"string","minLength":2,"maxLength":4,"pattern":"^[a-z]+$"},
	  "host":  {"type":"string","format":"hostname"},
	  "ip":    {"type":"string","format":"ipv4"},
	  "ratio": {"type":"number","minimum":0,"maximum":1},
	  "on":    {"type":"boolean"},
	  "ports": {"type":"array","items":{"type":"integer","minimum":1}},
	  "nested":{"type":"object","properties":{"x":{"type":"integer","default":3}}}
	}}`)
	good := map[string]any{"mode": "a", "name": "abc", "host": "cam-1.local", "ip": "10.0.0.1", "ratio": 0.5, "on": true, "ports": []any{1.0, 2.0}, "nested": map[string]any{}}
	effective, err := schema.Effective(good)
	if err != nil {
		t.Fatalf("Effective(good): %v", err)
	}
	if nested := effective["nested"].(map[string]any); nested["x"] != float64(3) {
		t.Fatalf("nested default not filled: %v", effective)
	}
	if _, present := good["nested"].(map[string]any)["x"]; present {
		t.Fatal("Effective changed its input")
	}
	for key, value := range map[string]any{
		"mode": "c", "name": "ABC", "host": "-bad", "ip": "::1", "ratio": 2.0, "on": "yes", "ports": []any{0.0},
	} {
		values := map[string]any{key: value}
		if _, err := schema.Effective(values); err == nil {
			t.Errorf("%s=%v accepted", key, value)
		}
	}
}

// TestInspectRefusesEverythingExtractRefuses is the invariant that makes the
// door meaningful: the same table TestExtractRejectsHostileEntries uses.
// If inspection ever accepts one of these, a hostile package reaches the
// catalog and fails later, on every node instead of once at upload.
func TestInspectRefusesEverythingExtractRefuses(t *testing.T) {
	for name, testCase := range hostileArchiveCases() {
		t.Run(name, func(t *testing.T) {
			path := craftZip(t, testCase.build)
			_, err := InspectPackage(path, testCase.limits)
			if err == nil {
				t.Fatal("inspection accepted an archive extraction refuses")
			}
			if !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("expected error containing %q, got %v", testCase.message, err)
			}
		})
	}
}

// TestInspectRefusesDeclaredBomb: the cheap pass. A tiny archive whose
// central directory admits it holds tens of megabytes is refused without
// decompressing anything — "declares" in the message is the proof that pass 1
// caught it, since the measured pass words the same verdict differently.
func TestInspectRefusesDeclaredBomb(t *testing.T) {
	path := craftZip(t, func(w *zip.Writer) {
		entry, _ := w.Create("zeros")
		zeros := make([]byte, 1<<20)
		for range 40 {
			if _, err := entry.Write(zeros); err != nil {
				t.Fatal(err)
			}
		}
	})
	_, err := InspectPackage(path, ArchiveLimits{MaxEntries: 100, MaxCompressionRatio: 200})
	if err == nil || !strings.Contains(err.Error(), "zip bomb") {
		t.Fatalf("declared bomb not refused: %v", err)
	}
	if !strings.Contains(err.Error(), "declares") {
		t.Fatalf("expected the declared-size pass to catch this, got %v", err)
	}
}

// TestUnderstatedArchiveCannotOutgrowItsDeclaration pins down WHY pass 1 is
// sound rather than a cheap guess, because the opposite is the natural
// assumption: surely a header can lie about being small and then decompress
// to something huge?
//
// It cannot. Go's zip reader stops at the declaration — archive/zip
// reader.go, checksumReader.Read returns ErrFormat as soon as nread exceeds
// UncompressedSize64. An understating archive does not smuggle bytes past the
// size cap; it just becomes unreadable. This test crafts exactly that archive
// (4096 real bytes declared as 100) and asserts both halves: inspection
// refuses it, AND the measured count never got past the declared one.
//
// If a future Go release drops that check, this test fails and the soundness
// claim in InspectPackage's comment has to be rewritten rather than trusted.
func TestUnderstatedArchiveCannotOutgrowItsDeclaration(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("liar")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(bytes.Repeat([]byte("x"), 4096)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	raw := patchDeclaredSize(t, buffer.Bytes(), 100)
	path := filepath.Join(t.TempDir(), "liar.tmod")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	limits := ArchiveLimits{MaxEntries: 100, MaxTotalBytes: 1024, MaxManifestBytes: 1 << 20, MaxCompressionRatio: 200}
	inspection, err := InspectPackage(path, limits)
	if err == nil {
		t.Fatal("an understating archive passed inspection")
	}

	if inspection.MeasuredBytes > inspection.DeclaredBytes {
		t.Fatalf("read %d bytes from an archive declaring %d — the zip reader no longer caps at the declaration, so pass 1 is not sound",
			inspection.MeasuredBytes, inspection.DeclaredBytes)
	}
}

// patchDeclaredSize rewrites the central directory's uncompressed-size field
// so the archive UNDERSTATES what it holds. The offset walk goes through the
// end-of-central-directory record rather than scanning for the signature,
// because compressed payload can contain those four bytes by chance.
func patchDeclaredSize(t *testing.T, raw []byte, claim uint32) []byte {
	t.Helper()
	eocd := bytes.LastIndex(raw, []byte("PK\x05\x06"))
	if eocd < 0 || eocd+20 > len(raw) {
		t.Fatal("no end-of-central-directory record")
	}

	directory := int(binary.LittleEndian.Uint32(raw[eocd+16 : eocd+20]))
	if directory <= 0 || directory+28 > len(raw) || !bytes.Equal(raw[directory:directory+4], []byte("PK\x01\x02")) {
		t.Fatalf("central directory not where the EOCD says (%d)", directory)
	}
	patched := append([]byte(nil), raw...)
	binary.LittleEndian.PutUint32(patched[directory+24:directory+28], claim)
	return patched
}

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

func TestParseManifestAcceptsCanonicalV2(t *testing.T) {
	manifest, err := ParseManifest([]byte(sampleV2))
	if err != nil {
		t.Fatalf("valid v2 rejected: %v", err)
	}
	if manifest.ID != "io.terra.backup" || manifest.Kind != KindService {
		t.Fatalf("unexpected identity: %#v", manifest)
	}
	if manifest.Entrypoints.Process["linux-amd64"] != "bin/linux-amd64/terra-backup" {
		t.Fatalf("process entrypoint not parsed: %#v", manifest.Entrypoints)
	}
	if manifest.Contracts["api"] != "contracts/api/terra-api.json" {
		t.Fatalf("contracts not parsed: %#v", manifest.Contracts)
	}
	if manifest.Update == nil || !manifest.Update.Rollback {
		t.Fatalf("update policy not parsed: %#v", manifest.Update)
	}
}

func TestParseManifestRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"unknownKind":  `{"schemaVersion":2,"id":"io.terra.x","version":"1.0.0","kind":"widget","entrypoints":{"process":{"linux-amd64":"bin/x"}}}`,
		"wrongSchema":  `{"schemaVersion":1,"id":"io.terra.x","version":"1.0.0","kind":"service","entrypoints":{"process":{"linux-amd64":"bin/x"}}}`,
		"badId":        `{"schemaVersion":2,"id":"BAD ID","version":"1.0.0","kind":"service","entrypoints":{"process":{"linux-amd64":"bin/x"}}}`,
		"noVersion":    `{"schemaVersion":2,"id":"io.terra.x","kind":"service","entrypoints":{"process":{"linux-amd64":"bin/x"}}}`,
		"noEntrypoint": `{"schemaVersion":2,"id":"io.terra.x","version":"1.0.0","kind":"service"}`,
	}
	for name, document := range cases {
		if _, err := ParseManifest([]byte(document)); err == nil {
			t.Fatalf("case %q: expected an error", name)
		}
	}
}

func TestParseManifestAllowsSceneWithoutEntrypoint(t *testing.T) {
	document := `{"schemaVersion":2,"id":"io.terra.scene.leaf","version":"1.0.0","kind":"scene"}`
	if _, err := ParseManifest([]byte(document)); err != nil {
		t.Fatalf("scene without process entrypoint rejected: %v", err)
	}
}

func TestParseManifestIntegrity(t *testing.T) {
	const digest = "511a81955c3b3a9e87e0f95156608f5c7a4515302e29a5ec3ed40be587d28ef7"
	valid := `{"schemaVersion":2,"id":"io.terra.x","version":"1.0.0","kind":"service",
		"entrypoints":{"process":{"linux-amd64":"bin/x"}},
		"integrity":{"process":{"linux-amd64":"` + digest + `"}}}`
	manifest, err := ParseManifest([]byte(valid))
	if err != nil {
		t.Fatalf("valid integrity rejected: %v", err)
	}
	if manifest.Integrity == nil || manifest.Integrity.Process["linux-amd64"] != digest {
		t.Fatalf("integrity digest not parsed: %#v", manifest.Integrity)
	}

	upper := `{"schemaVersion":2,"id":"io.terra.x","version":"1.0.0","kind":"service",
		"entrypoints":{"process":{"linux-amd64":"bin/x"}},
		"integrity":{"process":{"linux-amd64":" ` + "511A81955C3B3A9E87E0F95156608F5C7A4515302E29A5EC3ED40BE587D28EF7" + ` "}}}`
	manifest, err = ParseManifest([]byte(upper))
	if err != nil {
		t.Fatalf("uppercase digest rejected: %v", err)
	}
	if manifest.Integrity.Process["linux-amd64"] != digest {
		t.Fatalf("digest not normalized: %q", manifest.Integrity.Process["linux-amd64"])
	}

	invalid := map[string]string{
		"malformedDigest": `{"schemaVersion":2,"id":"io.terra.x","version":"1.0.0","kind":"service",
			"entrypoints":{"process":{"linux-amd64":"bin/x"}},
			"integrity":{"process":{"linux-amd64":"not-a-digest"}}}`,
		"orphanTarget": `{"schemaVersion":2,"id":"io.terra.x","version":"1.0.0","kind":"service",
			"entrypoints":{"process":{"linux-amd64":"bin/x"}},
			"integrity":{"process":{"windows-amd64":"` + digest + `"}}}`,
	}
	for name, document := range invalid {
		if _, err := ParseManifest([]byte(document)); err == nil {
			t.Fatalf("case %q: expected an error", name)
		}
	}
}

// TestParseManifestAllowsAdapterWrappingExternalService pins the rule the
// Gateway's adapter scan already relies on: an adapter whose implementation is
// a service running elsewhere carries a contract and an address, not an
// executable, so requiring an entrypoint would reject a package the Gateway
// then happily serves.
func TestParseManifestAllowsAdapterWrappingExternalService(t *testing.T) {
	document := `{"schemaVersion":2,"id":"io.terra.adapter.weather","version":"1.0.0","kind":"adapter",
		"contributions":{"service":{"providerId":"io.terra.weather","baseUrl":"http://127.0.0.1:9900"}}}`
	manifest, err := ParseManifest([]byte(document))
	if err != nil {
		t.Fatalf("adapter wrapping an external service rejected: %v", err)
	}
	if !ManifestWrapsExternalService(manifest) {
		t.Fatal("the service contribution should be recognised")
	}
}

// TestParseManifestRejectsEntrypointlessModulesWithoutImplementation pins the
// other edge of the same rule: the exemption is the service contribution, not
// the adapter kind, and it does not spread to kinds that must ship a process.
func TestParseManifestRejectsEntrypointlessModulesWithoutImplementation(t *testing.T) {
	cases := map[string]string{

		"adapterWithoutService": `{"schemaVersion":2,"id":"io.terra.adapter.x","version":"1.0.0","kind":"adapter"}`,

		"adapterMissingBaseURL": `{"schemaVersion":2,"id":"io.terra.adapter.x","version":"1.0.0","kind":"adapter",
			"contributions":{"service":{"providerId":"io.terra.x"}}}`,
		"adapterMissingProvider": `{"schemaVersion":2,"id":"io.terra.adapter.x","version":"1.0.0","kind":"adapter",
			"contributions":{"service":{"baseUrl":"http://127.0.0.1:9900"}}}`,

		"serviceWithServiceContribution": `{"schemaVersion":2,"id":"io.terra.x","version":"1.0.0","kind":"service",
			"contributions":{"service":{"providerId":"io.terra.x","baseUrl":"http://127.0.0.1:9900"}}}`,
	}
	for name, document := range cases {
		if _, err := ParseManifest([]byte(document)); err == nil {
			t.Fatalf("case %q: expected an error", name)
		}
	}
}

// Scene Extension 패키지 — 프로세스가 없고 구현을 기여가 가리킨다.
//
// 첫 Extension 모듈(io.terra.ext.metrics)을 실제로 만들다 나왔다. 게이트웨이의
// GUI 스캐너는 kind 를 보지 않아 끝에서 끝까지 돌았는데, **설치는 되지 않았다** —
// 여기 다섯 종류에 extension 이 없었다. 두 자리가 서로 다른 것을 알고 있었다.
func TestExtensionKindNeedsNoEntrypointButNeedsTheContribution(t *testing.T) {
	withContribution := []byte(`{
		"schemaVersion": 2, "id": "io.terra.ext.metrics", "version": "0.1.0", "kind": "extension",
		"contributions": { "gui": { "extensions": [
			{ "id": "io.terra.ext.metrics", "kind": "renderer", "entry": "ui/metrics.js" }
		] } }
	}`)
	if _, err := ParseManifest(withContribution); err != nil {
		t.Fatalf("Extension 패키지가 거부됐다: %v", err)
	}

	for name, raw := range map[string]string{
		"기여 블록이 없다": `{"schemaVersion":2,"id":"io.terra.ext.x","version":"0.1.0","kind":"extension"}`,
		"빈 배열":      `{"schemaVersion":2,"id":"io.terra.ext.x","version":"0.1.0","kind":"extension","contributions":{"gui":{"extensions":[]}}}`,
		"다른 기여만":    `{"schemaVersion":2,"id":"io.terra.ext.x","version":"0.1.0","kind":"extension","contributions":{"gui":{"apps":[{"id":"a"}]}}}`,
	} {
		if _, err := ParseManifest([]byte(raw)); err == nil {
			t.Errorf("%s: 통과했다 — 아무것도 기여하지 않는 Extension 패키지다", name)
		}
	}
}

const sampleV1 = `{
  "schema_version": 1,
  "id": "io.terra.virtual-device",
  "name": "Terra Virtual Device Bridge",
  "version": "0.1.0",
  "description": "Projects an SVI endpoint as a virtual device.",
  "kind": "application",
  "enabled": false,
  "auto_start": false,
  "entrypoints": {
    "linux-amd64": {"path": "bin/linux-amd64/terra-virtual-device", "sha256": "abc123"},
    "windows-amd64": {"path": "bin/windows-amd64/terra-virtual-device.exe"}
  },
  "args": ["--config", "config/virtual-device.json"],
  "restart": {"policy": "on-failure", "max_restarts": 3, "backoff_ms": 1000},
  "capabilities": {
    "resources": ["virtual_device.adapters"],
    "actions": ["virtual_device.slot.create"],
    "signals": ["virtual_device.status.changed"]
  },
  "permissions": ["daemon.svi.resource.read", "os.virtual-device.use"]
}`

func TestNormalizeV1PromotesIdentityAndEntrypoints(t *testing.T) {
	manifest, err := NormalizeV1Bytes([]byte(sampleV1))
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if manifest.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("schemaVersion not promoted: %d", manifest.SchemaVersion)
	}
	if manifest.ID != "io.terra.virtual-device" || manifest.Kind != KindApplication || manifest.Version != "0.1.0" {
		t.Fatalf("identity not promoted: %#v", manifest)
	}
	if manifest.Entrypoints.Process["linux-amd64"] != "bin/linux-amd64/terra-virtual-device" {
		t.Fatalf("v1 process entrypoint not promoted: %#v", manifest.Entrypoints)
	}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("normalized v2 manifest invalid: %v", err)
	}
}

func TestNormalizeV1PreservesAllLegacyFields(t *testing.T) {
	manifest, err := NormalizeV1Bytes([]byte(sampleV1))
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	legacy := manifest.Legacy
	if legacy == nil {
		t.Fatalf("legacy block missing")
	}
	if legacy.Restart == nil || legacy.Restart.Policy != "on-failure" || legacy.Restart.MaxRestarts != 3 || legacy.Restart.BackoffMS != 1000 {
		t.Fatalf("restart not preserved: %#v", legacy.Restart)
	}
	if legacy.Capabilities == nil || len(legacy.Capabilities.Actions) != 1 || legacy.Capabilities.Actions[0] != "virtual_device.slot.create" {
		t.Fatalf("capabilities not preserved: %#v", legacy.Capabilities)
	}
	if len(legacy.Permissions) != 2 {
		t.Fatalf("permissions not preserved: %#v", legacy.Permissions)
	}
	if legacy.EntrypointHashes["linux-amd64"] != "abc123" {
		t.Fatalf("entrypoint hash not preserved: %#v", legacy.EntrypointHashes)
	}
	if len(legacy.Args) != 2 || legacy.Args[0] != "--config" {
		t.Fatalf("args not preserved: %#v", legacy.Args)
	}
}

func TestNormalizeV1MapsPermissionsAndCapabilitiesToV2(t *testing.T) {
	manifest, err := NormalizeV1Bytes([]byte(sampleV1))
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	perms := manifest.Permissions
	if perms == nil {
		t.Fatalf("v2 permissions not populated")
	}
	if len(perms.CoreOperations) != 1 || perms.CoreOperations[0].ID != "daemon.svi.resource.read" {
		t.Fatalf("core operation permission not mapped: %#v", perms.CoreOperations)
	}
	if perms.CoreOperations[0].VersionRange != "" {
		t.Fatalf("v1 has no versionRange, want empty: %#v", perms.CoreOperations[0])
	}
	if len(perms.Native) != 1 || perms.Native[0] != "os.virtual-device.use" {
		t.Fatalf("native permission not mapped: %#v", perms.Native)
	}
	if len(perms.Storage) != 0 || len(perms.Network) != 0 || len(perms.Secret) != 0 {
		t.Fatalf("unexpected permission categories: %#v", perms)
	}

	provides := manifest.Provides
	if provides == nil || len(provides.Capabilities) != 2 {
		t.Fatalf("provides not mapped from resources+actions: %#v", provides)
	}
	got := map[string]string{}
	for _, capability := range provides.Capabilities {
		got[capability.ID] = capability.Version
	}
	if got["virtual_device.adapters"] != "0.1.0" || got["virtual_device.slot.create"] != "0.1.0" {
		t.Fatalf("resource/action not lifted at module version: %#v", provides.Capabilities)
	}
	if _, leaked := got["virtual_device.status.changed"]; leaked {
		t.Fatalf("signal must not become a provided capability: %#v", provides.Capabilities)
	}

	if manifest.Legacy == nil || len(manifest.Legacy.Permissions) != 2 || manifest.Legacy.Capabilities == nil {
		t.Fatalf("legacy v1 lists not preserved alongside v2 mapping: %#v", manifest.Legacy)
	}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("mapped v2 manifest invalid: %v", err)
	}
}

func TestDecodeV1RejectsNonV1(t *testing.T) {
	if _, err := DecodeV1([]byte(`{"schema_version":2,"id":"io.terra.x"}`)); err == nil {
		t.Fatalf("expected v1 decode to reject schema_version 2")
	}
}

func TestValidateRejectsMalformedParts(t *testing.T) {
	base := func(parts string) string {
		return `{"schemaVersion":2,"id":"io.terra.x","version":"1.0.0","kind":"service",
			"entrypoints":{"process":{"linux-amd64":"bin/x"}},
			"integrity":{` + parts + `}}`
	}
	digest := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	cases := map[string]string{
		"traversalPath":  base(`"parts":[{"name":"common","files":{"../evil":"` + digest + `"}}]`),
		"absolutePath":   base(`"parts":[{"name":"common","files":{"/etc/passwd":"` + digest + `"}}]`),
		"backslashPath":  base(`"parts":[{"name":"common","files":{"ui\\index.html":"` + digest + `"}}]`),
		"manifestListed": base(`"parts":[{"name":"common","files":{"module.json":"` + digest + `"}}]`),
		"badDigest":      base(`"parts":[{"name":"common","files":{"ui/index.html":"nothex"}}]`),
		"emptyPart":      base(`"parts":[{"name":"common","files":{}}]`),
		"badPartName":    base(`"parts":[{"name":"Common Part","files":{"ui/a":"` + digest + `"}}]`),
		"dupPartName":    base(`"parts":[{"name":"common","files":{"ui/a":"` + digest + `"}},{"name":"common","files":{"ui/b":"` + digest + `"}}]`),
		"dupFilePath":    base(`"parts":[{"name":"common","files":{"ui/a":"` + digest + `"}},{"name":"extra","files":{"ui/a":"` + digest + `"}}]`),
		"partShaWrong":   base(`"parts":[{"name":"common","sha256":"` + other + `","files":{"ui/a":"` + digest + `"}}]`),
		"processConflict": base(`"process":{"linux-amd64":"` + digest + `"},
			"parts":[{"name":"linux-amd64","files":{"bin/x":"` + other + `"}}]`),
	}
	for name, document := range cases {
		if _, err := ParseManifest([]byte(document)); err == nil {
			t.Fatalf("case %q: expected an error", name)
		}
	}

	agreeing := base(`"process":{"linux-amd64":"` + digest + `"},
		"parts":[{"name":"linux-amd64","files":{"bin/x":"` + digest + `"}}]`)
	if _, err := ParseManifest([]byte(agreeing)); err != nil {
		t.Fatalf("agreeing process/parts rejected: %v", err)
	}
}

func TestCanonicalPermissionsFlattensSortsAndDedupes(t *testing.T) {
	m := Manifest{Permissions: &Permissions{
		CoreOperations: []Dependency{
			{ID: "terra.fleet.slots.delete"},
			{Capability: "terra.node.identity"},
			{ID: " terra.fleet.slots.delete ", Capability: "ignored"},
		},
		Storage: []string{"module-data"},
		Network: []string{"api.example.com:443", "api.example.com:443"},
		Secret:  []string{"token"},
		Native:  []string{"gpu"},
	}}
	got, err := CanonicalPermissions(m)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"core.operation:terra.fleet.slots.delete",
		"core.operation:terra.node.identity",
		"native:gpu",
		"network:api.example.com:443",
		"secret:token",
		"storage:module-data",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCanonicalPermissionsEmptyIsNonNil(t *testing.T) {
	for _, m := range []Manifest{{}, {Permissions: &Permissions{}}} {
		got, err := CanonicalPermissions(m)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("got %#v, %v; want empty non-nil slice", got, err)
		}
	}
}

func TestCanonicalPermissionsRejectsAmbiguousDeclarations(t *testing.T) {
	for name, p := range map[string]*Permissions{
		"op id with colon":  {CoreOperations: []Dependency{{ID: "network:evil"}}},
		"op without a name": {CoreOperations: []Dependency{{VersionRange: ">=1"}}},
		"empty storage":     {Storage: []string{" "}},
		"empty network":     {Network: []string{""}},
	} {
		if _, err := CanonicalPermissionsOf(p); !errors.Is(err, ErrInvalidPermission) {
			t.Errorf("%s: err = %v, want ErrInvalidPermission", name, err)
		}
	}
}

func TestNormalizePermissionsReadsLegacyBareOperationIDs(t *testing.T) {
	got := NormalizePermissions([]string{"terra.fleet.slots.delete", "core.operation:terra.fleet.slots.delete", " network:a ", ""})
	want := []string{"core.operation:terra.fleet.slots.delete", "network:a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if NormalizePermissions(nil) != nil {
		t.Fatal("nil must stay nil")
	}
	if got := NormalizePermissions([]string{}); got == nil || len(got) != 0 {
		t.Fatalf("empty must stay empty non-nil, got %#v", got)
	}
}

func TestPermissionsNotIn(t *testing.T) {
	missing := PermissionsNotIn(
		[]string{"core.operation:a", "network:h", "storage:s"},
		[]string{"a", "storage:s"},
	)
	if !reflect.DeepEqual(missing, []string{"network:h"}) {
		t.Fatalf("missing = %v", missing)
	}
	if PermissionsNotIn([]string{"a"}, []string{"core.operation:a"}) != nil {
		t.Fatal("covered set must report nil")
	}
}
