package protocol

import "testing"

func TestValidNodeID(t *testing.T) {
	for _, id := range []string{"node_a", "node_1787923476493_4c773555dd1dd13d", " node_a.b-c "} {
		if !ValidNodeID(id) {
			t.Errorf("ValidNodeID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "node_", "node_-a", "nodea", "node_a/b", "Node_a"} {
		if ValidNodeID(id) {
			t.Errorf("ValidNodeID(%q) = true", id)
		}
	}
}
