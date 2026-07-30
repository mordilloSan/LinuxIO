package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestRouterSingletonAdmissionIsAtomic(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := SingletonSystem
	policy.Name = "atomic-singleton"
	release := make(chan struct{})
	route := Route{
		Name:   "test.atomic.singleton",
		Mode:   ModeJob,
		Policy: policy,
		Runner: func(context.Context, *Job, any) (any, error) {
			<-release
			return nil, nil
		},
	}

	const callers = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			<-start
			_, _, err := router.startOrQueueJob(route, Request{Route: route.Name})
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrDuplicateActive) {
			t.Fatalf("startOrQueueJob error = %v, want ErrDuplicateActive", err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d singleton jobs, want 1", successes)
	}
	close(release)
}

func TestRouterCanceledQueuedJobIsSkippedDuringPromotion(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := ActionDefault
	policy.Name = "cancel-promotion"
	policy.MaxActivePerRoute = 1
	policy.MaxActivePerOwnerRoute = 1
	policy.QueueLimit = 3

	activeRelease := make(chan struct{})
	nextRan := make(chan struct{})
	var canceledRuns atomic.Int32
	var nextRuns atomic.Int32
	route := Route{
		Name:   "test.cancel.promotion",
		Mode:   ModeJob,
		Policy: policy,
		Runner: func(_ context.Context, _ *Job, request any) (any, error) {
			requestName, _ := request.(string)
			switch requestName {
			case "active":
				<-activeRelease
			case "canceled":
				canceledRuns.Add(1)
			case "next":
				nextRuns.Add(1)
				close(nextRan)
			}
			return nil, nil
		},
	}

	active, started, err := router.startOrQueueJob(route, Request{
		Route:        route.Name,
		DecodedValue: "active",
	})
	if err != nil || !started {
		t.Fatalf("start active = (%v, %t, %v), want started job", active, started, err)
	}
	canceled, started, err := router.startOrQueueJob(route, Request{
		Route:        route.Name,
		DecodedValue: "canceled",
	})
	if err != nil || started {
		t.Fatalf("queue canceled = (%v, %t, %v), want queued job", canceled, started, err)
	}
	next, started, err := router.startOrQueueJob(route, Request{
		Route:        route.Name,
		DecodedValue: "next",
	})
	if err != nil || started {
		t.Fatalf("queue next = (%v, %t, %v), want queued job", next, started, err)
	}

	// Cancellation is deliberately issued while the first job still owns the
	// only active slot. Its completion immediately promotes the queue, which
	// must skip this terminal entry and run the next eligible job exactly once.
	canceled.Cancel()
	close(activeRelease)

	select {
	case <-nextRan:
	case <-time.After(time.Second):
		t.Fatal("next queued job was not promoted")
	}
	<-active.Done()
	<-canceled.Done()
	<-next.Done()

	if got := canceledRuns.Load(); got != 0 {
		t.Fatalf("canceled queued handler ran %d times, want 0", got)
	}
	if got := nextRuns.Load(); got != 1 {
		t.Fatalf("next queued handler ran %d times, want 1", got)
	}
	if state := canceled.Snapshot().State; state != StateCanceled {
		t.Fatalf("canceled job state = %q, want canceled", state)
	}

	deadline := time.Now().Add(time.Second)
	for {
		router.mu.RLock()
		activeCount := router.activeByRoute[route.Name]
		queuedCount := len(router.queuedByRoute[route.Name])
		pendingCount := router.pendingQueuedByRoute[route.Name]
		router.mu.RUnlock()
		if activeCount == 0 && queuedCount == 0 && pendingCount == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("router accounting did not settle: active=%d queued=%d pending=%d", activeCount, queuedCount, pendingCount)
		}
		time.Sleep(time.Millisecond)
	}
}

//nolint:gocognit // The explicit promotion race timeline is the test contract.
func TestRouterPromotionCancellationRefusesReservedJobStart(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := ActionDefault
	policy.Name = "cancel-after-promotion"
	policy.MaxActivePerRoute = 1
	policy.MaxActivePerOwnerRoute = 1
	policy.QueueLimit = 3

	activeRelease := make(chan struct{})
	promotionPaused := make(chan struct{})
	resumePromotion := make(chan struct{})
	nextRan := make(chan struct{})
	var candidateRuns atomic.Int32
	var nextRuns atomic.Int32
	route := Route{
		Name:   "test.cancel.after.promotion",
		Mode:   ModeJob,
		Policy: policy,
		Runner: func(_ context.Context, _ *Job, request any) (any, error) {
			requestName, _ := request.(string)
			switch requestName {
			case "active":
				<-activeRelease
			case "candidate":
				candidateRuns.Add(1)
			case "next":
				nextRuns.Add(1)
				close(nextRan)
			}
			return nil, nil
		},
	}

	active, started, err := router.startOrQueueJob(route, Request{
		Route:        route.Name,
		DecodedValue: "active",
	})
	if err != nil || !started {
		t.Fatalf("start active = (%v, %t, %v), want started job", active, started, err)
	}
	candidate, started, err := router.startOrQueueJob(route, Request{
		Route:        route.Name,
		DecodedValue: "candidate",
	})
	if err != nil || started {
		t.Fatalf("queue candidate = (%v, %t, %v), want queued job", candidate, started, err)
	}
	next, started, err := router.startOrQueueJob(route, Request{
		Route:        route.Name,
		DecodedValue: "next",
	})
	if err != nil || started {
		t.Fatalf("queue next = (%v, %t, %v), want queued job", next, started, err)
	}

	router.beforeStartHook = func(job *Job) {
		if job != candidate {
			return
		}
		close(promotionPaused)
		<-resumePromotion
	}
	close(activeRelease)
	select {
	case <-promotionPaused:
	case <-time.After(time.Second):
		t.Fatal("candidate was not reserved for promotion")
	}

	// The router has reserved candidate's active slot but has not called Start.
	// Cancel wins Job.mu, so Start must refuse and release this reservation once.
	candidate.Cancel()
	close(resumePromotion)
	select {
	case <-nextRan:
	case <-time.After(time.Second):
		t.Fatal("next queued job was not promoted after canceled reservation")
	}
	<-active.Done()
	<-candidate.Done()
	<-next.Done()

	if got := candidateRuns.Load(); got != 0 {
		t.Fatalf("canceled reserved handler ran %d times, want 0", got)
	}
	if got := nextRuns.Load(); got != 1 {
		t.Fatalf("next queued handler ran %d times, want 1", got)
	}
	if state := candidate.Snapshot().State; state != StateCanceled {
		t.Fatalf("candidate state = %q, want canceled", state)
	}

	deadline := time.Now().Add(time.Second)
	for {
		router.mu.RLock()
		activeCount := router.activeByRoute[route.Name]
		queuedCount := len(router.queuedByRoute[route.Name])
		pendingCount := router.pendingQueuedByRoute[route.Name]
		router.mu.RUnlock()
		if activeCount == 0 && queuedCount == 0 && pendingCount == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("router accounting did not settle: active=%d queued=%d pending=%d", activeCount, queuedCount, pendingCount)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRouterJobFastCompleteReturnsTerminalSnapshot(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	router.JobRunner("test.fast", func(ctx context.Context, job *Job, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	}, ActionDefault)

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, Request{Route: "test.fast"})
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult {
		t.Fatalf("opcode = 0x%02x, want OpStreamResult", frame.Opcode)
	}

	var result relay.ResultFrame
	if unmarshalErr := json.Unmarshal(frame.Payload, &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(result): %v", unmarshalErr)
	}
	if result.Status != "ok" {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	var snapshot Snapshot
	if unmarshalErr := json.Unmarshal(result.Data, &snapshot); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(snapshot): %v", unmarshalErr)
	}
	if snapshot.State != StateCompleted {
		t.Fatalf("state = %q, want completed", snapshot.State)
	}
	if snapshot.Result == nil {
		t.Fatal("fast-complete snapshot missing result")
	}
	closeFrame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if closeFrame.Opcode != relay.OpStreamClose {
		t.Fatalf("close opcode = 0x%02x, want OpStreamClose", closeFrame.Opcode)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
}

func TestRouterJobTimeoutReturnsFailedSnapshot(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := ActionDefault
	policy.Name = "timeout_test"
	policy.Timeout = 10 * time.Millisecond
	router.JobRunner("test.timeout", func(ctx context.Context, job *Job, _ any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, policy)

	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, Request{Route: "test.timeout"})
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult {
		t.Fatalf("opcode = 0x%02x, want OpStreamResult", frame.Opcode)
	}

	var result relay.ResultFrame
	if unmarshalErr := json.Unmarshal(frame.Payload, &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(result): %v", unmarshalErr)
	}
	if result.Status != "ok" {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	var snapshot Snapshot
	if unmarshalErr := json.Unmarshal(result.Data, &snapshot); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(snapshot): %v", unmarshalErr)
	}
	if snapshot.State != StateFailed {
		t.Fatalf("state = %q, want failed", snapshot.State)
	}
	if snapshot.Error == nil {
		t.Fatal("timeout snapshot missing error")
	}
	if snapshot.Error.Code != 504 {
		t.Fatalf("error code = %d, want 504", snapshot.Error.Code)
	}
	if !strings.Contains(snapshot.Error.Message, "timed out") {
		t.Fatalf("error message = %q, want timeout message", snapshot.Error.Message)
	}

	closeFrame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if closeFrame.Opcode != relay.OpStreamClose {
		t.Fatalf("close opcode = 0x%02x, want OpStreamClose", closeFrame.Opcode)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
}

func TestRouterRejectsRegisteredJobsNamespace(t *testing.T) {
	router := NewRouter(NewRegistry())
	defer func() {
		if recover() == nil {
			t.Fatal("expected reserved jobs.* registration to panic")
		}
	}()
	router.Query("jobs.get", func(ctx context.Context, _ any, emit Events) error {
		return emit.Result(nil)
	})
}
