package redis

import "testing"

func TestParseReplicationInfo(t *testing.T) {
	got := parseInfo("# Replication\r\nrole:slave\r\nmaster_replid:abc123\r\nslave_repl_offset:42\r\n")
	if got["role"] != "slave" {
		t.Fatalf("role = %q, want slave", got["role"])
	}
	if got["master_replid"] != "abc123" {
		t.Fatalf("master_replid = %q, want abc123", got["master_replid"])
	}
}

func TestValueInt64(t *testing.T) {
	for _, value := range []interface{}{int64(42), 42, "42", []byte("42")} {
		if got := valueInt64(value); got != 42 {
			t.Fatalf("valueInt64(%T(%v)) = %d, want 42", value, value, got)
		}
	}
}
