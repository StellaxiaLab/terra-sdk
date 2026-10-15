package conformance

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ReportSchemaVersion is the version of the JSON report shape. Fields are only
// ever added within a version.
const ReportSchemaVersion = 1

// JSONResult is one rule in the JSON report.
type JSONResult struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
	Rule   string `json:"rule,omitempty"`
}

// JSONReport is the machine-readable form of a Report, as printed by
// `terra-conformance -json`.
type JSONReport struct {
	SchemaVersion int `json:"schemaVersion"`
	// OK is true when no rule failed and at least one passed.
	OK bool `json:"ok"`
	// WireContract is the wire contract version the kit checked against.
	WireContract int `json:"wireContract"`
	// Error is set when the harness itself failed (exit status 2); Results
	// then holds whatever was checked before the failure.
	Error   string       `json:"error,omitempty"`
	Results []JSONResult `json:"results"`
}

// ToJSON converts the report. harnessErr, when non-nil, is recorded in Error
// and forces OK to false.
func (r Report) ToJSON(harnessErr error) JSONReport {
	out := JSONReport{
		SchemaVersion: ReportSchemaVersion,
		OK:            harnessErr == nil && r.OK(),
		WireContract:  WireContractVersion,
		Results:       make([]JSONResult, 0, len(r.Results)),
	}
	if harnessErr != nil {
		out.Error = harnessErr.Error()
	}
	for _, result := range r.Results {
		item := JSONResult{Name: result.Name, Status: result.Level(), Rule: result.Rule}
		if result.Err != nil {
			item.Error = result.Err.Error()
		}
		out.Results = append(out.Results, item)
	}
	return out
}

// MarshalJSON renders the report as indented JSON.
func (r Report) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToJSON(nil))
}

// Counts returns how many results are at each status.
func (r Report) Counts() map[Status]int {
	counts := map[Status]int{}
	for _, result := range r.Results {
		counts[result.Level()]++
	}
	return counts
}

// Summary is a one-line tally such as "12 pass, 0 fail, 1 warn, 2 skip".
func (r Report) Summary() string {
	counts := r.Counts()
	var parts []string
	for _, s := range []Status{StatusPass, StatusFail, StatusWarn, StatusSkip} {
		parts = append(parts, strconv.Itoa(counts[s])+" "+string(s))
	}
	return strings.Join(parts, ", ")
}
