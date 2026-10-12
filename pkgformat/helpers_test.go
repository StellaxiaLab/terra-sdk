package pkgformat

import (
	"reflect"
	"testing"
)

func TestValidModuleID(t *testing.T) {
	for id, want := range map[string]bool{
		"io.terra.file": true, "abc": true, "a-b_c.d": true,
		"ab": false, "": false, "Io.terra": false, "-abc": false, "a b c": false,
	} {
		if got := ValidModuleID(id); got != want {
			t.Errorf("ValidModuleID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestCloneConfigValueIsDeep(t *testing.T) {
	orig := map[string]any{"a": []any{"x", map[string]any{"k": 1.0}}, "b": "s"}
	clone := CloneConfigValue(orig).(map[string]any)
	if !reflect.DeepEqual(orig, clone) {
		t.Fatalf("clone differs: %v", clone)
	}
	clone["a"].([]any)[1].(map[string]any)["k"] = 2.0
	if orig["a"].([]any)[1].(map[string]any)["k"] != 1.0 {
		t.Fatal("clone shares nested storage with the original")
	}
}

func TestDecodeManifestAcceptsV1AndV2(t *testing.T) {
	v1 := sampleV1
	if m, ver, err := DecodeManifest([]byte(v1)); err != nil || ver != 1 || m.ID != "io.terra.virtual-device" || m.Legacy == nil {
		t.Fatalf("v1: %+v ver=%d err=%v", m, ver, err)
	}
	v2 := `{"schemaVersion":2,"id":"io.terra.v2","name":"V2","version":"1.0.0","kind":"service","entrypoints":{"process":{"linux-amd64":"bin/x"}}}`
	if m, ver, err := DecodeManifest([]byte(v2)); err != nil || ver != 2 || m.ID != "io.terra.v2" {
		t.Fatalf("v2: %+v ver=%d err=%v", m, ver, err)
	}
	if _, _, err := DecodeManifest([]byte(`{"id":"x"}`)); err == nil {
		t.Fatal("a manifest without a schema version must be refused")
	}
}
