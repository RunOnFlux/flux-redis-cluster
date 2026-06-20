package redis

import "testing"

func TestSentinelFieldsFromArray(t *testing.T) {
	fields, err := sentinelFields([]interface{}{
		"name", "redis-test",
		"ip", "81.111.245.69",
		"port", "16379",
	})
	if err != nil {
		t.Fatalf("sentinelFields returned error: %v", err)
	}
	if fields["ip"] != "81.111.245.69" || fields["port"] != "16379" {
		t.Fatalf("sentinelFields = %v", fields)
	}
}

func TestSentinelFieldsFromMap(t *testing.T) {
	fields, err := sentinelFields(map[interface{}]interface{}{
		"name": "redis-test",
		"ip":   "81.111.245.69",
		"port": int64(16379),
	})
	if err != nil {
		t.Fatalf("sentinelFields returned error: %v", err)
	}
	if fields["ip"] != "81.111.245.69" || fields["port"] != "16379" {
		t.Fatalf("sentinelFields = %v", fields)
	}
}
