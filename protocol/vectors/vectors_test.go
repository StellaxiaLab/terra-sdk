package vectors

import (
	"encoding/json"
	"io/fs"
	"testing"
)

func TestEveryVectorIsEmbeddedJSON(t *testing.T) {
	want := []string{
		"terraid/tokens.json", "terraid/jwks.json", "terraid/key_bundle.json", "terraid/test_key.json",
		"terraname/vectors.json", "terraname/record_vectors.json",
	}
	for _, name := range want {
		raw, err := fs.ReadFile(FS, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !json.Valid(raw) {
			t.Errorf("%s is not valid JSON", name)
		}
	}
}
