package main

import (
	"reflect"
	"testing"
)

func TestBtrfsTimerArgs(t *testing.T) {
	got := btrfsTimerArgs("btrfs-snapshot", []string{"run", "--now", "2026-10-19T10:00:00Z"})
	want := []string{btrfsTimerBinary, "snapshot", "run", "--now", "2026-10-19T10:00:00Z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot args = %v, want %v", got, want)
	}
	got = btrfsTimerArgs("btrfs-health", []string{"run"})
	want = []string{btrfsTimerBinary, "health", "run"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("health args = %v, want %v", got, want)
	}
}
