package scheduledrun

import (
	"errors"
	"fmt"
	"testing"
)

func TestAllReported(t *testing.T) {
	cause := errors.New("pull failed")
	reported := Reported(fmt.Errorf("web: %w", cause))
	other := errors.New("docker is not reachable")
	for name, tt := range map[string]struct {
		err  error
		want bool
	}{
		"no failure":                 {nil, false},
		"reported":                   {reported, true},
		"reported, wrapped":          {fmt.Errorf("update: %w", reported), true},
		"not reported":               {other, false},
		"all joined are reported":    {errors.Join(reported, Reported(other)), true},
		"one joined is not":          {errors.Join(reported, other), false},
		"nested join with one not":   {fmt.Errorf("pass: %w", errors.Join(reported, errors.Join(nil, other))), false},
		"two wrapped with one not":   {fmt.Errorf("%w: %w", reported, other), false},
		"reported keeps its content": {reported, true},
	} {
		if got := AllReported(tt.err); got != tt.want {
			t.Errorf("%s: AllReported = %v, want %v", name, got, tt.want)
		}
	}
	if Reported(nil) != nil || Skipped(nil) != nil {
		t.Error("a marked nil is not nil")
	}
	for name, tt := range map[string]struct {
		err  error
		want int
	}{
		"all reported": {fmt.Errorf("pass: %w", reported), ExitReported},
		"one is not":   {errors.Join(reported, other), 1},
		"skipped":      {fmt.Errorf("pass: %w", Skipped(other)), ExitSkipped},
		"not marked":   {other, 1},
	} {
		if got := ExitStatus(tt.err); got != tt.want {
			t.Errorf("%s: ExitStatus = %d, want %d", name, got, tt.want)
		}
	}
	if !errors.Is(reported, cause) || reported.Error() != "web: pull failed" {
		t.Errorf("Reported changed the error: %v", reported)
	}
}
