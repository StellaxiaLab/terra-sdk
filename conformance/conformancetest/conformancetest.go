// Package conformancetest runs the conformance kit from a Go test.
package conformancetest

import (
	"context"
	"testing"

	"github.com/StellaxiaLab/terra-sdk/conformance"
)

// Run launches the module described by spec and fails t for every contract rule
// it breaks. Typical use from a module's own tests:
//
//	conformancetest.Run(t, conformance.Spec{Command: "python3", Args: []string{"module.py"}})
func Run(t testing.TB, spec conformance.Spec) {
	t.Helper()
	report, err := conformance.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("conformance harness: %v", err)
	}
	for _, result := range report.Results {
		if result.Err != nil {
			t.Errorf("%s: %v", result.Name, result.Err)
		}
	}
	if len(report.Results) == 0 {
		t.Fatal("conformance: no rules were checked")
	}
}
