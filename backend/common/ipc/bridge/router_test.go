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

func TestRouterOwnerStartRateLimitStillEnforced(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := JobPolicy{
		Name:                    "rate-limited",
		MaxActivePerRoute:       8,
		MaxActivePerOwnerRoute:  8,
		StartRatePerMinuteOwner: 2,
	}
	route := Route{
		Name:   "test.rate.limited",
		Mode:   ModeJob,
		Policy: policy,
		Runner: func(context.Context, *Job, any) (any, error) { return nil, nil },
	}

	for i := range 2 {
		job, _, err := router.startOrQueueJob(route, Request{Route: route.Name})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		<-job.Done()
	}
	if _, _, err := router.startOrQueueJob(route, Request{Route: route.Name}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third start error = %v, want ErrRateLimited", err)
	}
}

func TestRouterSkipsStartHistoryWhenOwnerRateLimitDisabled(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := ActionDefault // StartRatePerMinuteOwner: 0
	policy.Name = "rate-disabled"
	route := Route{
		Name:   "test.rate.disabled",
		Mode:   ModeJob,
		Policy: policy,
		Runner: func(context.Context, *Job, any) (any, error) { return nil, nil },
	}

	for i := range 3 {
		job, _, err := router.startOrQueueJob(route, Request{Route: route.Name})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		<-job.Done()
	}

	// checkRateLocked never prunes for a disabled limit, so admission must not
	// record start history at all — it would grow for the bridge lifetime.
	ownerRouteKey := route.Name + "\x00" + Owner{}.key()
	router.mu.RLock()
	tracked := len(router.startsByOwnerRoute[ownerRouteKey])
	router.mu.RUnlock()
	if tracked != 0 {
		t.Fatalf("recorded %d start timestamps with the owner rate limit disabled, want 0", tracked)
	}
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

	waitForRouterSettle(t, router, route.Name)
}

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

	waitForRouterSettle(t, router, route.Name)
}

func TestRouterCanceledDetachedRunnerKeepsAdmissionSlotUntilExit(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := ActionDefault
	policy.Name = "cancel-detached-runner"
	policy.MaxActivePerRoute = 1
	policy.MaxActivePerOwnerRoute = 1
	policy.QueueLimit = 1
	policy.Timeout = time.Second

	started := make(chan struct{})
	release := make(chan struct{})
	successorStarted := make(chan struct{})
	var running atomic.Int32
	var maxRunning atomic.Int32
	updateMax := func(current int32) {
		for {
			observed := maxRunning.Load()
			if current <= observed || maxRunning.CompareAndSwap(observed, current) {
				return
			}
		}
	}
	route := Route{
		Name:   "test.cancel.detached-runner",
		Mode:   ModeJob,
		Policy: policy,
		Runner: func(_ context.Context, _ *Job, request any) (any, error) {
			current := running.Add(1)
			updateMax(current)
			defer running.Add(-1)
			if request == "active" {
				close(started)
				<-release // Deliberately ignore the canceled context.
				return nil, nil
			}
			close(successorStarted)
			return nil, nil
		},
	}

	active, startedNow, err := router.startOrQueueJob(route, Request{Route: route.Name, DecodedValue: "active"})
	if err != nil || !startedNow {
		t.Fatalf("start active = (%v, %t, %v), want started job", active, startedNow, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active handler did not start")
	}
	successor, startedNow, err := router.startOrQueueJob(route, Request{Route: route.Name, DecodedValue: "successor"})
	if err != nil || startedNow {
		t.Fatalf("queue successor = (%v, %t, %v), want queued job", successor, startedNow, err)
	}

	active.Cancel()
	select {
	case <-active.Done():
	case <-time.After(time.Second):
		t.Fatal("canceled job did not publish terminal state")
	}
	if state := active.Snapshot().State; state != StateCanceled {
		t.Fatalf("active state = %q, want canceled", state)
	}
	assertChannelOpen(t, successorStarted, "successor started while canceled handler still ran")
	if got := running.Load(); got != 1 {
		t.Fatalf("running handlers = %d, want 1 before release", got)
	}

	close(release)
	select {
	case <-successorStarted:
	case <-time.After(time.Second):
		t.Fatal("successor was not promoted after handler exit")
	}
	<-successor.Done()
	if got := maxRunning.Load(); got != 1 {
		t.Fatalf("maximum concurrent handlers = %d, want 1", got)
	}
	waitForRouterSettle(t, router, route.Name)
}

func TestRouterTimedOutDetachedRunnerKeepsAdmissionSlotUntilExit(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := ActionDefault
	policy.Name = "timeout-detached-runner"
	policy.MaxActivePerRoute = 1
	policy.MaxActivePerOwnerRoute = 1
	policy.QueueLimit = 1
	policy.Timeout = 10 * time.Millisecond

	started := make(chan struct{})
	release := make(chan struct{})
	successorStarted := make(chan struct{})
	var running atomic.Int32
	var maxRunning atomic.Int32
	updateMax := func(current int32) {
		for {
			observed := maxRunning.Load()
			if current <= observed || maxRunning.CompareAndSwap(observed, current) {
				return
			}
		}
	}
	route := Route{
		Name:   "test.timeout.detached-runner",
		Mode:   ModeJob,
		Policy: policy,
		Runner: func(_ context.Context, _ *Job, request any) (any, error) {
			current := running.Add(1)
			updateMax(current)
			defer running.Add(-1)
			if request == "active" {
				close(started)
				<-release // Deliberately ignore the timeout context.
				return nil, nil
			}
			close(successorStarted)
			return nil, nil
		},
	}

	active, startedNow, err := router.startOrQueueJob(route, Request{Route: route.Name, DecodedValue: "active"})
	if err != nil || !startedNow {
		t.Fatalf("start active = (%v, %t, %v), want started job", active, startedNow, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active handler did not start")
	}
	successor, startedNow, err := router.startOrQueueJob(route, Request{Route: route.Name, DecodedValue: "successor"})
	if err != nil || startedNow {
		t.Fatalf("queue successor = (%v, %t, %v), want queued job", successor, startedNow, err)
	}

	select {
	case <-active.Done():
	case <-time.After(time.Second):
		t.Fatal("timed-out job did not publish terminal state")
	}
	if snapshot := active.Snapshot(); snapshot.State != StateFailed || snapshot.Error == nil || snapshot.Error.Code != 504 {
		t.Fatalf("timeout snapshot = %+v, want failed 504", snapshot)
	}
	assertChannelOpen(t, successorStarted, "successor started while timed-out handler still ran")
	if got := running.Load(); got != 1 {
		t.Fatalf("running handlers = %d, want 1 before release", got)
	}

	close(release)
	select {
	case <-successorStarted:
	case <-time.After(time.Second):
		t.Fatal("successor was not promoted after handler exit")
	}
	<-successor.Done()
	if got := maxRunning.Load(); got != 1 {
		t.Fatalf("maximum concurrent handlers = %d, want 1", got)
	}
	waitForRouterSettle(t, router, route.Name)
}

func TestRouterDirectRunnerReleasesAdmissionSlotOnExit(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	policy := ActionDefault
	policy.Name = "direct-runner"
	policy.MaxActivePerRoute = 1
	policy.MaxActivePerOwnerRoute = 1
	policy.QueueLimit = 1
	policy.Timeout = 0

	started := make(chan struct{})
	release := make(chan struct{})
	successorStarted := make(chan struct{})
	route := Route{
		Name:   "test.direct-runner",
		Mode:   ModeJob,
		Policy: policy,
		Runner: func(_ context.Context, _ *Job, request any) (any, error) {
			if request == "active" {
				close(started)
				<-release
				return nil, nil
			}
			close(successorStarted)
			return nil, nil
		},
	}

	active, startedNow, err := router.startOrQueueJob(route, Request{Route: route.Name, DecodedValue: "active"})
	if err != nil || !startedNow {
		t.Fatalf("start active = (%v, %t, %v), want started job", active, startedNow, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active handler did not start")
	}
	successor, startedNow, err := router.startOrQueueJob(route, Request{Route: route.Name, DecodedValue: "successor"})
	if err != nil || startedNow {
		t.Fatalf("queue successor = (%v, %t, %v), want queued job", successor, startedNow, err)
	}

	assertChannelOpen(t, successorStarted, "successor started before direct handler returned")
	close(release)
	<-active.Done()
	select {
	case <-successorStarted:
	case <-time.After(time.Second):
		t.Fatal("successor was not promoted after direct handler exit")
	}
	<-successor.Done()
	waitForRouterSettle(t, router, route.Name)
}

func assertChannelOpen(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(message)
	case <-time.After(25 * time.Millisecond):
	}
}

// waitForRouterSettle polls until the router's accounting for a route drains
// to zero (no active, queued, or pending-queued entries) or fails the test.
func waitForRouterSettle(t *testing.T, router *Router, routeName string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		router.mu.RLock()
		activeCount := router.activeByRoute[routeName]
		queuedCount := len(router.queuedByRoute[routeName])
		pendingCount := router.pendingQueuedByRoute[routeName]
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
