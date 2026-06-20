package main

import (
	"reflect"
	"testing"
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

func TestStaleIPsNoStaleEntries(t *testing.T) {
	got := staleIPs(
		[]string{"50.29.138.5", "80.72.20.158"},
		[]string{"50.29.138.5", "80.72.20.158"},
	)
	if len(got) != 0 {
		t.Fatalf("staleIPs = %v, want no stale IPs", got)
	}
}
