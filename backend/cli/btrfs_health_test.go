package main

import (
	"context"
	"errors"
	"testing"
)

func TestRunBtrfsHealth(t *testing.T) {
	old := checkBtrfsHealth
	t.Cleanup(func() { checkBtrfsHealth = old })
	runs := 0
	var fail error
	checkBtrfsHealth = func(context.Context) error {
		runs++
		return fail
	}
	for _, args := range [][]string{nil, {"bogus"}, {"run", "extra"}} {
		if code := runBtrfsHealth(context.Background(), args); code != 2 {
			t.Errorf("runBtrfsHealth(%v) = %d, want 2", args, code)
		}
	}
	if runs != 0 {
		t.Fatalf("checker ran on bad usage: %d", runs)
	}
	if code := runBtrfsHealth(context.Background(), []string{"run"}); code != 0 || runs != 1 {
		t.Fatalf("run = %d, runs = %d", code, runs)
	}
	fail = errors.New("boom")
	if code := runBtrfsHealth(context.Background(), []string{"run"}); code != 1 {
		t.Fatalf("failed run = %d, want 1", code)
	}
}
