package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"
)

// -json prints exactly one JSON document on stdout, even when the harness
// fails, and the exit status stays 2.
func TestJSONOnHarnessError(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = writer
	code := run([]string{"-json", "-launches", "1", "--", "terra-conformance-no-such-command-xyz"})
	os.Stdout = stdout
	_ = writer.Close()
	data, _ := io.ReadAll(reader)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	var report struct {
		SchemaVersion int    `json:"schemaVersion"`
		OK            bool   `json:"ok"`
		Error         string `json:"error"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%s", err, data)
	}
	if report.SchemaVersion != 1 || report.OK || report.Error == "" {
		t.Fatalf("unexpected report: %s", data)
	}
}
