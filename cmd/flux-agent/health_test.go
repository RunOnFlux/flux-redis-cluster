package main

import "testing"

func TestHasAnyFlag(t *testing.T) {
	if !hasAnyFlag("master,s_down,disconnected", "s_down") {
		t.Fatal("expected s_down to be detected")
	}
	if hasAnyFlag("master", "s_down", "o_down", "disconnected") {
		t.Fatal("did not expect healthy master flags to be detected as unhealthy")
	}
}
