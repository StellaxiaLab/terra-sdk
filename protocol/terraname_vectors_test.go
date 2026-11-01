package protocol

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The vectors are the contract a service repository checks its own code
// against, through the vectors package.
func TestTerraNameGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("vectors", "terraname", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Valid []struct {
			In, Label, Tree, Handle, FQDN, Zone string
		} `json:"valid"`
		Invalid []struct {
			In, Why string
		} `json:"invalid"`
		Classify []struct {
			In   string      `json:"in"`
			Kind NodeRefKind `json:"kind"`
		} `json:"classify"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors.Valid {
		got, err := ParseTerraName(v.In)
		if err != nil {
			t.Errorf("ParseTerraName(%q) = %v", v.In, err)
			continue
		}
		if got != (TerraName{Label: v.Label, Tree: v.Tree, Handle: v.Handle}) || got.String() != v.FQDN || got.Zone() != v.Zone {
			t.Errorf("ParseTerraName(%q) = %+v (%s, %s), want %s %s %s / %s / %s",
				v.In, got, got.String(), got.Zone(), v.Label, v.Tree, v.Handle, v.FQDN, v.Zone)
		}
	}
	for _, v := range vectors.Invalid {
		if _, err := ParseTerraName(v.In); !errors.Is(err, ErrTerraNameInvalid) {
			t.Errorf("ParseTerraName(%q) [%s] = %v, want ErrTerraNameInvalid", v.In, v.Why, err)
		}
	}
	for _, v := range vectors.Classify {
		if got := ClassifyNodeRef(v.In); got != v.Kind {
			t.Errorf("ClassifyNodeRef(%q) = %q, want %q", v.In, got, v.Kind)
		}
	}
}
