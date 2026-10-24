package collector

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestShouldReseed(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		liveAt      time.Time
		collectorAt time.Time
		want        bool
	}{
		{"no collector baseline", now.Add(-time.Hour), time.Time{}, false},
		{"live key without baseline", time.Time{}, now.Add(-2 * time.Second), true},
		{"stale live key", now.Add(-time.Hour), now.Add(-2 * time.Second), true},
		{"live key newer than collector", now.Add(-time.Second), now.Add(-2 * time.Second), false},
		{"collector tick inside the minimum window", now.Add(-time.Hour), now.Add(-100 * time.Millisecond), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldReseed(tt.liveAt, tt.collectorAt, now); got != tt.want {
				t.Fatalf("ShouldReseed = %v, want %v", got, tt.want)
			}
		})
	}
}

func usableWithinMinute(r *Run[int]) bool { return r.Fresh(time.Minute) }

func TestFlightSharesOneCollection(t *testing.T) {
	var flight Flight[string, int]
	var collects atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	owner := make(chan *Run[int], 1)
	go func() {
		run, err := flight.Do(context.Background(), "k", 0, usableWithinMinute, func(r *Run[int]) {
			collects.Add(1)
			close(started)
			<-release
			r.Value = 7
		})
		if err != nil {
			t.Error(err)
		}
		owner <- run
	}()
	<-started

	joined := make(chan *Run[int], 1)
	go func() {
		run, err := flight.Do(context.Background(), "k", 0, usableWithinMinute, func(*Run[int]) {
			t.Error("second caller collected instead of joining")
		})
		if err != nil {
			t.Error(err)
		}
		joined <- run
	}()
	close(release)

	first, second := <-owner, <-joined
	if first != second || first.Value != 7 || collects.Load() != 1 {
		t.Fatalf("run shared = %v, value = %d, collections = %d", first == second, first.Value, collects.Load())
	}
}

func TestFlightRetriesAfterSharedFailure(t *testing.T) {
	var flight Flight[string, int]
	failed := flight.Begin("k", 0)
	done := make(chan *Run[int], 1)
	go func() {
		run, err := flight.Do(context.Background(), "k", 0, usableWithinMinute, func(r *Run[int]) { r.Value = 9 })
		if err != nil {
			t.Error(err)
		}
		done <- run
	}()
	failed.Err = errors.New("boom")
	failed.Finish()

	if got := <-done; got == failed || got.Value != 9 {
		t.Fatalf("joiner did not collect its own run: %+v", got)
	}
}

func TestFlightDoReturnsOwnerError(t *testing.T) {
	var flight Flight[string, int]
	boom := errors.New("boom")
	if _, err := flight.Do(context.Background(), "k", 0, usableWithinMinute, func(r *Run[int]) { r.Err = boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	run, owned := flight.Join("k", 0, usableWithinMinute)
	if !owned || run.Err != nil {
		t.Fatalf("a failed run must not be reused: owned=%v err=%v", owned, run.Err)
	}
}

func TestFlightEndOnlyRemovesTheRegisteredRun(t *testing.T) {
	var flight Flight[string, int]
	old := flight.Begin("k", 0)
	current := flight.Begin("k", 0)
	flight.End("k", old)
	if flight.Current("k") != current {
		t.Fatal("End removed a run that is no longer registered")
	}
	flight.End("k", current)
	if flight.Current("k") != nil {
		t.Fatal("End kept the registered run")
	}
}
