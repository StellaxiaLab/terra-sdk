package protocol

import (
	"encoding/json"
	"errors"
	"testing"
)

const hostedAccessTree = "node_1787923476493_4c773555dd1dd13d"

// Terra keeps the companion check that these intents are not yet control
// instructions (terra-protocol/hosted_access_intents_test.go).
func TestHostedAccessIntents(t *testing.T) {
	for _, intent := range []string{IntentNodeEnrollCodeIssue, IntentNodeEnrollExchange, IntentExternalIdentityRevoke} {
		if !HostedAccessIntent(intent) {
			t.Errorf("HostedAccessIntent(%q) = false", intent)
		}
	}
	if HostedAccessIntent("crossboundary.tree.describe") || HostedAccessIntent("process.execute.request") {
		t.Error("HostedAccessIntent accepted an unrelated intent")
	}
}

func TestNodeEnrollCodeIssueInstructionShape(t *testing.T) {
	valid := NodeEnrollCodeIssueInstruction{
		TreeNodeID:   hostedAccessTree,
		Assertion:    "eyJ.x.y",
		ParentNodeID: "node_1787923476494_aa11bb22cc33dd44",
		MasterURL:    "https://tree.example:8443",
		ExpiresInSec: 900,
	}
	if err := ValidateNodeEnrollCodeIssueInstruction(valid); err != nil {
		t.Fatalf("valid instruction rejected: %v", err)
	}

	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	var back NodeEnrollCodeIssueInstruction
	if err := json.Unmarshal(raw, &back); err != nil || back != valid {
		t.Fatalf("round trip = %+v, %v", back, err)
	}

	cases := map[string]func(*NodeEnrollCodeIssueInstruction){
		"tree not a node id":   func(i *NodeEnrollCodeIssueInstruction) { i.TreeNodeID = "lab" },
		"no assertion":         func(i *NodeEnrollCodeIssueInstruction) { i.Assertion = "  " },
		"parent not a node id": func(i *NodeEnrollCodeIssueInstruction) { i.ParentNodeID = "desk" },
		// H4: a delegated request has no origin a leaf can reach.
		"master url missing":    func(i *NodeEnrollCodeIssueInstruction) { i.MasterURL = "" },
		"master url relative":   func(i *NodeEnrollCodeIssueInstruction) { i.MasterURL = "/api" },
		"master url wrong type": func(i *NodeEnrollCodeIssueInstruction) { i.MasterURL = "wss://tree.example" },
		"master url with query": func(i *NodeEnrollCodeIssueInstruction) { i.MasterURL = "https://tree.example/?a=b" },
		"master url with user":  func(i *NodeEnrollCodeIssueInstruction) { i.MasterURL = "https://u:p@tree.example" },
		"ttl negative":          func(i *NodeEnrollCodeIssueInstruction) { i.ExpiresInSec = -1 },
		"ttl over cap":          func(i *NodeEnrollCodeIssueInstruction) { i.ExpiresInSec = MaxDelegatedEnrollCodeTTLSec + 1 },
	}
	for name, mutate := range cases {
		instruction := valid
		mutate(&instruction)
		if err := ValidateNodeEnrollCodeIssueInstruction(instruction); !errors.Is(err, ErrInvalidDelegation) {
			t.Errorf("%s: err = %v, want ErrInvalidDelegation", name, err)
		}
	}

	rootPlacement := valid
	rootPlacement.ParentNodeID = ""
	rootPlacement.ExpiresInSec = 0
	if err := ValidateNodeEnrollCodeIssueInstruction(rootPlacement); err != nil {
		t.Errorf("root placement with default ttl rejected: %v", err)
	}
}

func TestNodeEnrollExchangeInstructionShape(t *testing.T) {
	valid := NodeEnrollExchangeInstruction{
		TreeNodeID:    hostedAccessTree,
		Enrollment:    json.RawMessage(`{"code":"tnc_x","os":"linux"}`),
		ClientAddress: "203.0.113.7",
	}
	if err := ValidateNodeEnrollExchangeInstruction(valid); err != nil {
		t.Fatalf("valid instruction rejected: %v", err)
	}
	for name, body := range map[string]string{"empty": "", "array": `[]`, "string": `"x"`, "null": `null`, "broken": `{`} {
		instruction := valid
		instruction.Enrollment = json.RawMessage(body)
		if err := ValidateNodeEnrollExchangeInstruction(instruction); !errors.Is(err, ErrInvalidDelegation) {
			t.Errorf("%s enrollment: err = %v, want ErrInvalidDelegation", name, err)
		}
	}
	instruction := valid
	instruction.TreeNodeID = ""
	if err := ValidateNodeEnrollExchangeInstruction(instruction); !errors.Is(err, ErrInvalidDelegation) {
		t.Errorf("missing tree: err = %v", err)
	}

	answer := NodeEnrollExchangeAnswer{HTTPStatus: 201, Body: json.RawMessage(`{"node_id":"node_1"}`)}
	raw, _ := json.Marshal(answer)
	if string(raw) != `{"http_status":201,"body":{"node_id":"node_1"}}` {
		t.Errorf("answer wire form = %s", raw)
	}
}

func TestExternalIdentityRevokeInstructionShape(t *testing.T) {
	valid := ExternalIdentityRevokeInstruction{TreeNodeID: hostedAccessTree, Assertion: "eyJ.x.y"}
	if err := ValidateExternalIdentityRevokeInstruction(valid); err != nil {
		t.Fatalf("valid instruction rejected: %v", err)
	}
	if err := ValidateExternalIdentityRevokeInstruction(ExternalIdentityRevokeInstruction{TreeNodeID: hostedAccessTree}); !errors.Is(err, ErrInvalidDelegation) {
		t.Errorf("missing assertion: err = %v", err)
	}
	if err := ValidateExternalIdentityRevokeInstruction(ExternalIdentityRevokeInstruction{Assertion: "a"}); !errors.Is(err, ErrInvalidDelegation) {
		t.Errorf("missing tree: err = %v", err)
	}
	raw, _ := json.Marshal(ExternalIdentityRevokeAnswer{Suspended: true})
	if string(raw) != `{"suspended":true}` {
		t.Errorf("answer wire form = %s", raw)
	}
}

func TestTreeReachabilityValues(t *testing.T) {
	for _, value := range []TreeReachability{"", "public", "lan_only", "mesh_only"} {
		if !value.Valid() {
			t.Errorf("%q should be valid", value)
		}
	}
	if TreeReachability("internet").Valid() {
		t.Error("an unknown reachability was accepted")
	}
}

func TestCheckEnrollCodeIssueBinding(t *testing.T) {
	parent := "node_1787923476494_aa11bb22cc33dd44"
	root := ""
	instruction := NodeEnrollCodeIssueInstruction{TreeNodeID: hostedAccessTree, ParentNodeID: parent}
	if err := CheckEnrollCodeIssueBinding(TerraIDClaims{}, instruction); err != nil {
		t.Errorf("unpinned assertion: %v", err)
	}
	if err := CheckEnrollCodeIssueBinding(TerraIDClaims{ParentNodeID: &parent}, instruction); err != nil {
		t.Errorf("matching pin: %v", err)
	}
	if err := CheckEnrollCodeIssueBinding(TerraIDClaims{ParentNodeID: &root}, instruction); !errors.Is(err, ErrInvalidDelegation) {
		t.Errorf("root pin vs named parent: err = %v", err)
	}
	instruction.ParentNodeID = ""
	if err := CheckEnrollCodeIssueBinding(TerraIDClaims{ParentNodeID: &root}, instruction); err != nil {
		t.Errorf("root pin vs root: %v", err)
	}
}
