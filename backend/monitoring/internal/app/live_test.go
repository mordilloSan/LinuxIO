package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/live"
)

func TestLiveCurrentDataReusesWithinWindow(t *testing.T) {
	a := &App{}
	var collections atomic.Int32
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error) {
		collections.Add(1)
		return &systemmodel.CombinedData{}, nil
	}

	first, firstAt, err := a.liveCurrentData(context.Background(), 1010, false, false)
	if err != nil {
		t.Fatal(err)
	}
	second, secondAt, err := a.liveCurrentData(context.Background(), 1010, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if collections.Load() != 1 || first != second || !firstAt.Equal(secondAt) {
		t.Fatalf("expected one shared collection, got %d", collections.Load())
	}

	a.liveRuns.Current(1010).At = time.Now().Add(-2 * live.ReuseWindow)
	if _, _, err := a.liveCurrentData(context.Background(), 1010, false, false); err != nil {
		t.Fatal(err)
	}
	if collections.Load() != 2 {
		t.Fatalf("stale sample must recollect, got %d collections", collections.Load())
	}
}

func TestLiveCurrentDataSharesInFlightCollection(t *testing.T) {
	a := &App{}
	release := make(chan struct{})
	var collections atomic.Int32
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error) {
		collections.Add(1)
		<-release
		return &systemmodel.CombinedData{}, nil
	}

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, _, err := a.liveCurrentData(context.Background(), 1001, false, true); err != nil {
				t.Error(err)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if collections.Load() != 1 {
		t.Fatalf("concurrent callers must share one collection, got %d", collections.Load())
	}
}

func TestLiveCurrentDataDoesNotReuseNarrowerSample(t *testing.T) {
	a := &App{}
	var collections atomic.Int32
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error) {
		collections.Add(1)
		return &systemmodel.CombinedData{}, nil
	}
	if _, _, err := a.liveCurrentData(context.Background(), 1001, false, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.liveCurrentData(context.Background(), 1001, false, true); err != nil {
		t.Fatal(err)
	}
	if collections.Load() != 2 {
		t.Fatalf("a sample without containers must not satisfy a request with containers, got %d", collections.Load())
	}
}

func TestLiveCurrentDataInFlightWaitRespectsContext(t *testing.T) {
	a := &App{}
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error) {
		close(started)
		<-release
		return &systemmodel.CombinedData{}, nil
	}

	go func() {
		_, _, _ = a.liveCurrentData(context.Background(), 1000, false, false)
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := a.liveCurrentData(ctx, 1000, false, false); err == nil {
		t.Fatal("a cancelled waiter must not block on the in-flight collection")
	}
}

func TestLiveCurrentDataRetriesAfterSharedFailure(t *testing.T) {
	a := &App{}
	release := make(chan struct{})
	started := make(chan struct{})
	var collections atomic.Int32
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error) {
		if collections.Add(1) == 1 {
			close(started)
			<-release
			return nil, errors.New("collection failed")
		}
		return &systemmodel.CombinedData{}, nil
	}

	owner := make(chan error, 1)
	go func() {
		_, _, err := a.liveCurrentData(context.Background(), 1010, false, false)
		owner <- err
	}()
	<-started

	joiner := make(chan error, 1)
	go func() {
		_, _, err := a.liveCurrentData(context.Background(), 1010, false, false)
		joiner <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)

	if err := <-owner; err == nil {
		t.Fatal("the owning caller must see the collection error")
	}
	if err := <-joiner; err != nil {
		t.Fatalf("the joining caller must recollect after a shared failure: %v", err)
	}
	if collections.Load() != 2 {
		t.Fatalf("expected a retry collection, got %d", collections.Load())
	}
}

func TestLiveCurrentDataDetachesManagerOwnedPointers(t *testing.T) {
	a := &App{}
	// Stand-ins for dm.containerStatsMap and fsManager.fsStats entries, which
	// the next collection for any key rewrites in place.
	shared := &containermodel.Stats{Id: "abc", Cpu: 1}
	sharedFs := &systemmodel.FsStats{Mountpoint: "/data", DiskUsed: 1}
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error) {
		data := &systemmodel.CombinedData{Containers: []*containermodel.Stats{shared}}
		data.Stats.ExtraFs = map[string]*systemmodel.FsStats{"data": sharedFs}
		return data, nil
	}

	first, firstAt, err := a.liveCurrentData(context.Background(), 1010, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if first.Containers[0] == shared {
		t.Fatal("published sample must not alias the manager's container stats")
	}
	if first.Stats.ExtraFs["data"] == sharedFs {
		t.Fatal("published sample must not alias the manager's filesystem stats")
	}

	shared.Cpu = 99
	sharedFs.DiskUsed = 99

	second, secondAt, err := a.liveCurrentData(context.Background(), 1010, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if second != first || !secondAt.Equal(firstAt) {
		t.Fatal("expected the sample to be reused inside the window")
	}
	if second.Containers[0].Cpu != 1 {
		t.Fatalf("reused sample changed under captured_at: cpu = %v", second.Containers[0].Cpu)
	}
	if second.Stats.ExtraFs["data"].DiskUsed != 1 {
		t.Fatalf("reused sample changed under captured_at: disk used = %v", second.Stats.ExtraFs["data"].DiskUsed)
	}
}

func TestLiveCurrentDataPublishesRunOnPanic(t *testing.T) {
	a := &App{}
	var collections atomic.Int32
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error) {
		if collections.Add(1) == 1 {
			panic("collector exploded")
		}
		return &systemmodel.CombinedData{}, nil
	}

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Error("the panic must propagate to the caller")
			}
		}()
		_, _, _ = a.liveCurrentData(context.Background(), 1010, false, false)
	}()

	// The deadline is only a safety net: an unpublished run would block here.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := a.liveCurrentData(ctx, 1010, false, false); err != nil {
		t.Fatalf("a panicked run must not block later requests: %v", err)
	}
	if collections.Load() != 2 {
		t.Fatalf("expected a fresh collection after the panic, got %d", collections.Load())
	}
}

func TestLiveUsesOwnSampleKeyAndTelemetryMemo(t *testing.T) {
	a := &App{}
	var gotKey uint16
	var gotDetails, gotContainers bool
	a.collectLive = func(_ context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error) {
		gotKey, gotDetails, gotContainers = key, includeDetails, includeContainers
		return &systemmodel.CombinedData{
			Details:    &systemmodel.Details{Threads: 2},
			Containers: []*containermodel.Stats{{Id: "abc", Cpu: 10}},
		}, nil
	}
	a.rememberTelemetry([]containermodel.Telemetry{{ID: "abc", DiskReadBytesPerSecond: 5}}, time.Now())

	payload, err := a.Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != liveLiveSampleKey || !gotDetails || !gotContainers {
		t.Fatalf("key = %d details = %v containers = %v", gotKey, gotDetails, gotContainers)
	}
	ctr := payload.Containers.Items[0]
	if ctr.CPUPercent != 20 {
		t.Fatalf("threads multiplier not applied: %+v", ctr)
	}
	if ctr.BlockReadBytesPerSec == nil || *ctr.BlockReadBytesPerSec != 5 {
		t.Fatalf("memoed telemetry not attached: %+v", ctr)
	}
}
