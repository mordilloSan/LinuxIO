package scheduledrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

func endWithOutput(t *testing.T, write func(w io.Writer)) (*fakeHooks, string) {
	t.Helper()
	h := newHooks(t)
	h.Output = func(_ context.Context, invocationID string, w io.Writer) error {
		if invocationID != testInvocation {
			t.Errorf("output of invocation %q", invocationID)
		}
		write(w)
		return nil
	}
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"success", "exited", "0"}); err != nil {
		t.Fatalf("End: %v", err)
	}
	return h, h.Store.OutputPath(testRunID)
}

func TestEndSavesTheOutput(t *testing.T) {
	_, path := endWithOutput(t, func(w io.Writer) { _, _ = io.WriteString(w, "first\nlast\n") })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\nlast\n" {
		t.Errorf("output = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestEndWithNoOutputWritesNoFile(t *testing.T) {
	_, path := endWithOutput(t, func(io.Writer) {})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Stat = %v, want not exist", err)
	}
}

func TestEndKeepsTheStartAndEndOfALongOutput(t *testing.T) {
	const mib = 1 << 20
	_, path := endWithOutput(t, func(w io.Writer) {
		for _, part := range []struct {
			b byte
			n int
		}{{'A', 5 * mib}, {'M', 2 * mib}, {'Z', 5 * mib}} {
			_, _ = w.Write(bytes.Repeat([]byte{part.b}, part.n))
		}
	})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	half := jobs.MaxOutputBytes / 2
	gap := []byte("\n[… 2097152 bytes left out …]\n")
	want := append(append(bytes.Repeat([]byte{'A'}, half), gap...), bytes.Repeat([]byte{'Z'}, half)...)
	if !bytes.Equal(data, want) {
		t.Errorf("output of %d bytes differs from the head, gap and tail expected (%d bytes)", len(data), len(want))
	}
}

func TestEndKeepsAnOutputOfExactlyTheLimit(t *testing.T) {
	_, path := endWithOutput(t, func(w io.Writer) {
		for range 10 {
			_, _ = w.Write(bytes.Repeat([]byte{'x'}, 1<<20))
		}
	})
	if info, err := os.Stat(path); err != nil || info.Size() != jobs.MaxOutputBytes {
		t.Errorf("size = %v, %v; want %d", info, err, jobs.MaxOutputBytes)
	}
}

func TestEndTrimsALongOutputWrittenInChunks(t *testing.T) {
	const chunk = 32 << 10
	total := 22<<20 + 12345
	stream := make([]byte, total)
	for i := range stream {
		stream[i] = byte(i%251) ^ byte(i/chunk)
	}
	_, path := endWithOutput(t, func(w io.Writer) {
		for rest := stream; len(rest) > 0; {
			n := min(chunk, len(rest))
			_, _ = w.Write(rest[:n])
			rest = rest[n:]
		}
	})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	half := jobs.MaxOutputBytes / 2
	gap := fmt.Appendf(nil, "\n[… %d bytes left out …]\n", total-jobs.MaxOutputBytes)
	want := append(append(append([]byte(nil), stream[:half]...), gap...), stream[total-half:]...)
	if !bytes.Equal(data, want) {
		t.Errorf("output of %d bytes differs from the first and last %d bytes with the gap (%d bytes)", len(data), half, len(want))
	}
}

func TestEndSavesWhatItReadWhenTheOutputFails(t *testing.T) {
	h := newHooks(t)
	failure := errors.New("journal gone")
	h.Output = func(_ context.Context, _ string, w io.Writer) error {
		_, _ = io.WriteString(w, "first\n")
		return failure
	}
	mustBegin(t, h, testUnit, testInvocation)
	err := h.End(t.Context(), testUnit, testInvocation, Result{"success", "exited", "0"})
	if !errors.Is(err, failure) {
		t.Fatalf("End = %v, want the output error", err)
	}
	data, readErr := os.ReadFile(h.Store.OutputPath(testRunID))
	if readErr != nil || string(data) != "first\n" {
		t.Errorf("output = %q, %v; want first", data, readErr)
	}
}
