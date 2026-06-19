package config

import "testing"

func TestRedisTarget_LocalUsesContainerPort(t *testing.T) {
	cfg := &Config{
		MyIP:          "77.132.58.19",
		HostRedisPort: 16157,
		RedisPort:     6379,
	}
	host, port := cfg.RedisTarget("77.132.58.19")
	if host != "127.0.0.1" || port != 6379 {
		t.Fatalf("local target = %s:%d, want 127.0.0.1:6379", host, port)
	}
}

func TestRedisTarget_RemoteUsesHostPort(t *testing.T) {
	cfg := &Config{
		MyIP:          "77.132.58.19",
		HostRedisPort: 16157,
		RedisPort:     6379,
	}
	host, port := cfg.RedisTarget("80.72.20.158")
	if host != "80.72.20.158" || port != 16157 {
		t.Fatalf("remote target = %s:%d, want 80.72.20.158:16157", host, port)
	}
}

func TestSentinelMasterEndpoint_LocalUsesAdvertisedAddress(t *testing.T) {
	cfg := &Config{
		MyIP:          "77.132.58.19",
		HostRedisPort: 16157,
		RedisPort:     6379,
	}
	host, port := cfg.SentinelMasterEndpoint("77.132.58.19")
	if host != "77.132.58.19" || port != 16157 {
		t.Fatalf("local sentinel master = %s:%d, want 77.132.58.19:16157", host, port)
	}
}

func TestSentinelMasterEndpoint_RemoteUsesHostPort(t *testing.T) {
	cfg := &Config{
		MyIP:          "77.132.58.19",
		HostRedisPort: 16157,
		RedisPort:     6379,
	}
	host, port := cfg.SentinelMasterEndpoint("80.72.20.158")
	if host != "80.72.20.158" || port != 16157 {
		t.Fatalf("remote sentinel master = %s:%d, want 80.72.20.158:16157", host, port)
	}
}
