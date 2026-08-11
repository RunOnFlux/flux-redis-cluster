package main

import (
	"reflect"
	"testing"

	redisinternal "flux-redis-cluster/internal/redis"
)

func TestStaleIPs(t *testing.T) {
	got := staleIPs(
		[]string{"50.29.138.5", "77.132.58.19", "80.72.20.158"},
		[]string{"50.29.138.5", "80.72.20.158"},
	)
	want := []string{"77.132.58.19"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("staleIPs = %v, want %v", got, want)
	}
}

func TestSelectRecoveryCandidateUsesHighestOffset(t *testing.T) {
	states := []redisinternal.NodeState{
		{IP: "172.20.0.11", Role: "slave", Offset: 120, ReplicationID: "history-a"},
		{IP: "172.20.0.12", Role: "slave", Offset: 175, ReplicationID: "history-a"},
	}

	got, err := selectRecoveryCandidate(states)
	if err != nil {
		t.Fatal(err)
	}
	if got.IP != "172.20.0.12" {
		t.Fatalf("candidate = %s, want freshest node 172.20.0.12", got.IP)
	}
}

func TestSelectRecoveryCandidateTieBreaksDeterministically(t *testing.T) {
	states := []redisinternal.NodeState{
		{IP: "172.20.0.12", Role: "slave", Offset: 175},
		{IP: "172.20.0.11", Role: "slave", Offset: 175},
	}

	got, err := selectRecoveryCandidate(states)
	if err != nil {
		t.Fatal(err)
	}
	if got.IP != "172.20.0.11" {
		t.Fatalf("candidate = %s, want lowest IP tie-break 172.20.0.11", got.IP)
	}
}

func TestSelectRecoveryCandidateKeepsExistingMasterOnOffsetTie(t *testing.T) {
	states := []redisinternal.NodeState{
		{IP: "172.20.0.11", Role: "slave", Offset: 175},
		{IP: "172.20.0.12", Role: "master", Offset: 175},
	}

	got, err := selectRecoveryCandidate(states)
	if err != nil {
		t.Fatal(err)
	}
	if got.IP != "172.20.0.12" {
		t.Fatalf("candidate = %s, want existing master 172.20.0.12", got.IP)
	}
}

func TestStaleIPsNoStaleEntries(t *testing.T) {
	got := staleIPs(
		[]string{"50.29.138.5", "80.72.20.158"},
		[]string{"50.29.138.5", "80.72.20.158"},
	)
	if len(got) != 0 {
		t.Fatalf("staleIPs = %v, want no stale IPs", got)
	}
}
