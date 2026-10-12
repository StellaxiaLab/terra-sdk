package pkgformat

import (
	"encoding/json"
	"errors"
	"fmt"
)

// DecodeManifest decodes a single module manifest document, accepting either a
// canonical v2 manifest or a legacy v1 manifest (which it normalizes to v2). It
// returns the v2 manifest and the on-disk schema version (1 or 2). The result
// is validated.
func DecodeManifest(data []byte) (Manifest, int, error) {
	var peek struct {
		V2 int `json:"schemaVersion"`
		V1 int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return Manifest{}, 0, fmt.Errorf("module manifest JSON을 해석할 수 없습니다: %w", err)
	}
	switch {
	case peek.V2 == ManifestSchemaVersion:
		manifest, err := ParseManifest(data)
		return manifest, 2, err
	case peek.V1 == 1:
		manifest, err := NormalizeV1Bytes(data)
		if err != nil {
			return Manifest{}, 1, err
		}
		if err := manifest.Validate(); err != nil {
			return Manifest{}, 1, err
		}
		return manifest, 1, nil
	default:
		return Manifest{}, 0, errors.New("알 수 없는 manifest schema version (schemaVersion 2 또는 schema_version 1 필요)")
	}
}
