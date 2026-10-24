package main

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestRunHealth(t *testing.T) {
	old := checkBtrfsHealth
	t.Cleanup(func() { checkBtrfsHealth = old })
	runs := 0
	var fail error
	checkBtrfsHealth = func(context.Context) error {
		runs++
		return fail
	}
	for _, args := range [][]string{nil, {"bogus"}, {"run", "extra"}} {
		if code := runHealth(context.Background(), args, io.Discard); code != 2 {
			t.Errorf("runHealth(%v) = %d, want 2", args, code)
		}
	}
	if runs != 0 {
		t.Fatalf("checker ran on bad usage: %d", runs)
	}
	if code := runHealth(context.Background(), []string{"run"}, io.Discard); code != 0 || runs != 1 {
		t.Fatalf("run = %d, runs = %d", code, runs)
	}
	fail = errors.New("boom")
	if code := runHealth(context.Background(), []string{"run"}, io.Discard); code != 1 {
		t.Fatalf("failed run = %d, want 1", code)
	}
}

func TestRunCLIDispatch(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"snapshot"}, {"health"}} {
		if code := runCLI(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Errorf("runCLI(%v) = %d, want 2", args, code)
		}
	}
	if code := runCLI(context.Background(), []string{"help"}, io.Discard, io.Discard); code != 0 {
		t.Errorf("help = %d, want 0", code)
	}
}
