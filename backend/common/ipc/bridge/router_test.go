package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestRouterSingletonAdmissionIsAtomic(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskSingletonSystem
	policy.Name = "atomic-singleton"
	release := make(chan struct{})
	route := Route{
		Name:   "test.atomic.singleton",
		Mode:   ModeTask,
		Policy: policy,
		Runner: func(context.Context, *Task, any) (any, error) {
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
			_, _, err := router.startOrQueueTask(route, Request{Route: route.Name})
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
			t.Fatalf("startOrQueueTask error = %v, want ErrDuplicateActive", err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d singleton tasks, want 1", successes)
	}
	close(release)
}

func TestRouterConcurrentStableIdentityStartsOnlyOnce(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	release := make(chan struct{})
	runnerStarted := make(chan struct{})
	var runs atomic.Int32
	route := Route{
		Name:     "control.app_update",
		Mode:     ModeTask,
		Policy:   TaskSingletonSystem,
		Lifetime: TaskLifetimeDurable,
		Identity: func(request any) (TaskIdentity, error) {
			fingerprint, ok := request.(string)
			if !ok {
				return TaskIdentity{}, fmt.Errorf("unexpected identity request %T", request)
			}
			return TaskIdentity{ID: "00000000-0000-4000-8000-000000000042", Fingerprint: fingerprint}, nil
		},
		Runner: func(context.Context, *Task, any) (any, error) {
			runs.Add(1)
			close(runnerStarted)
			<-release
			return nil, nil
		},
	}
	request := Request{
		Route:        route.Name,
		DecodedValue: "same-request",
		Owner:        TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000},
	}

	type startResult struct {
		task *Task
		err  error
	}
	results := make(chan startResult, 2)
	var starts sync.WaitGroup
	starts.Add(2)
	for range 2 {
		go func() {
			defer starts.Done()
			task, _, err := router.startOrQueueTask(route, request)
			results <- startResult{task: task, err: err}
		}()
	}
	starts.Wait()
	close(results)
	var claimed []*Task
	for result := range results {
		if result.err != nil {
			t.Fatalf("start error: %v", result.err)
		}
		claimed = append(claimed, result.task)
	}
	if len(claimed) != 2 || claimed[0] != claimed[1] {
		t.Fatalf("concurrent claims = %v", claimed)
	}
	<-runnerStarted
	if got := runs.Load(); got != 1 {
		t.Fatalf("runner starts = %d, want 1", got)
	}
	close(release)
	<-claimed[0].Done()
}

func TestRouterRecoveredDurableTaskIsCancelableAndBlocksSingletonStart(t *testing.T) {
	type identityRequest struct {
		id          string
		fingerprint string
	}
	registry := NewTaskService()
	router := NewRouter(registry)
	runnerStarted := make(chan struct{})
	route := Route{
		Name:     "control.app_update",
		Mode:     ModeTask,
		Policy:   TaskSingletonSystem,
		Lifetime: TaskLifetimeDurable,
		Identity: func(request any) (TaskIdentity, error) {
			value, ok := request.(identityRequest)
			if !ok {
				return TaskIdentity{}, fmt.Errorf("unexpected identity request %T", request)
			}
			return TaskIdentity{ID: value.id, Fingerprint: value.fingerprint}, nil
		},
		Runner: func(ctx context.Context, _ *Task, _ any) (any, error) {
			close(runnerStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	router.register(route)
	owner := TaskOwner{SessionID: "new-session", Username: "alice", UID: 1000}
	identity := TaskIdentity{ID: "00000000-0000-4000-8000-000000000042", Fingerprint: "same-request"}
	task, created, err := router.RecoverDurableTask(route.Name, identityRequest{id: identity.ID, fingerprint: identity.Fingerprint}, owner, identity)
	if err != nil || !created {
		t.Fatalf("RecoverDurableTask = %v, %t, %v", task, created, err)
	}
	<-runnerStarted
	if _, _, err := router.startOrQueueTask(route, Request{
		Route: route.Name,
		DecodedValue: identityRequest{
			id:          "00000000-0000-4000-8000-000000000043",
			fingerprint: "different-request",
		},
		Owner: owner,
	}); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("new singleton start error = %v, want ErrDuplicateActive", err)
	}
	task.Cancel()
	<-task.Done()
	if state := task.Snapshot().State; state != TaskStateCanceled {
		t.Fatalf("recovered task state = %q, want canceled", state)
	}
}

func TestRouterOwnerStartRateLimitStillEnforced(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskPolicy{
		Name:                    "rate-limited",
		MaxActivePerRoute:       8,
		MaxActivePerOwnerRoute:  8,
		StartRatePerMinuteOwner: 2,
	}
	route := Route{
		Name:   "test.rate.limited",
		Mode:   ModeTask,
		Policy: policy,
		Runner: func(context.Context, *Task, any) (any, error) { return nil, nil },
	}

	for i := range 2 {
		task, _, err := router.startOrQueueTask(route, Request{Route: route.Name})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		<-task.Done()
	}
	if _, _, err := router.startOrQueueTask(route, Request{Route: route.Name}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third start error = %v, want ErrRateLimited", err)
	}
}

func TestRouterSkipsStartHistoryWhenOwnerRateLimitDisabled(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskDefault // StartRatePerMinuteOwner: 0
	policy.Name = "rate-disabled"
	route := Route{
		Name:   "test.rate.disabled",
		Mode:   ModeTask,
		Policy: policy,
		Runner: func(context.Context, *Task, any) (any, error) { return nil, nil },
	}

	for i := range 3 {
		task, _, err := router.startOrQueueTask(route, Request{Route: route.Name})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		<-task.Done()
	}

	// checkRateLocked never prunes for a disabled limit, so admission must not
	// record start history at all — it would grow for the bridge lifetime.
	ownerRouteKey := route.Name + "\x00" + TaskOwner{}.key(TaskLifetimeSession)
	router.mu.RLock()
	tracked := len(router.startsByOwnerRoute[ownerRouteKey])
	router.mu.RUnlock()
	if tracked != 0 {
		t.Fatalf("recorded %d start timestamps with the owner rate limit disabled, want 0", tracked)
	}
}

func TestRouterCanceledQueuedTaskIsSkippedDuringPromotion(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskDefault
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
		Mode:   ModeTask,
		Policy: policy,
		Runner: func(_ context.Context, _ *Task, request any) (any, error) {
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

	active, started, err := router.startOrQueueTask(route, Request{
		Route:        route.Name,
		DecodedValue: "active",
	})
	if err != nil || !started {
		t.Fatalf("start active = (%v, %t, %v), want started task", active, started, err)
	}
	canceled, started, err := router.startOrQueueTask(route, Request{
		Route:        route.Name,
		DecodedValue: "canceled",
	})
	if err != nil || started {
		t.Fatalf("queue canceled = (%v, %t, %v), want queued task", canceled, started, err)
	}
	next, started, err := router.startOrQueueTask(route, Request{
		Route:        route.Name,
		DecodedValue: "next",
	})
	if err != nil || started {
		t.Fatalf("queue next = (%v, %t, %v), want queued task", next, started, err)
	}

	// Cancellation is deliberately issued while the first task still owns the
	// only active slot. Its completion immediately promotes the queue, which
	// must skip this terminal entry and run the next eligible task exactly once.
	canceled.Cancel()
	close(activeRelease)

	select {
	case <-nextRan:
	case <-time.After(time.Second):
		t.Fatal("next queued task was not promoted")
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
	if state := canceled.Snapshot().State; state != TaskStateCanceled {
		t.Fatalf("canceled task state = %q, want canceled", state)
	}

	waitForRouterSettle(t, router, route.Name)
}

func TestRouterPromotionCancellationRefusesReservedTaskStart(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskDefault
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
		Mode:   ModeTask,
		Policy: policy,
		Runner: func(_ context.Context, _ *Task, request any) (any, error) {
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

	active, started, err := router.startOrQueueTask(route, Request{
		Route:        route.Name,
		DecodedValue: "active",
	})
	if err != nil || !started {
		t.Fatalf("start active = (%v, %t, %v), want started task", active, started, err)
	}
	candidate, started, err := router.startOrQueueTask(route, Request{
		Route:        route.Name,
		DecodedValue: "candidate",
	})
	if err != nil || started {
		t.Fatalf("queue candidate = (%v, %t, %v), want queued task", candidate, started, err)
	}
	next, started, err := router.startOrQueueTask(route, Request{
		Route:        route.Name,
		DecodedValue: "next",
	})
	if err != nil || started {
		t.Fatalf("queue next = (%v, %t, %v), want queued task", next, started, err)
	}

	router.beforeStartHook = func(task *Task) {
		if task != candidate {
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
	// Cancel wins Task.mu, so Start must refuse and release this reservation once.
	candidate.Cancel()
	close(resumePromotion)
	select {
	case <-nextRan:
	case <-time.After(time.Second):
		t.Fatal("next queued task was not promoted after canceled reservation")
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
	if state := candidate.Snapshot().State; state != TaskStateCanceled {
		t.Fatalf("candidate state = %q, want canceled", state)
	}

	waitForRouterSettle(t, router, route.Name)
}

func TestRouterCanceledDetachedRunnerKeepsAdmissionSlotUntilExit(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskDefault
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
		Mode:   ModeTask,
		Policy: policy,
		Runner: func(_ context.Context, _ *Task, request any) (any, error) {
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

	active, startedNow, err := router.startOrQueueTask(route, Request{Route: route.Name, DecodedValue: "active"})
	if err != nil || !startedNow {
		t.Fatalf("start active = (%v, %t, %v), want started task", active, startedNow, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active handler did not start")
	}
	successor, startedNow, err := router.startOrQueueTask(route, Request{Route: route.Name, DecodedValue: "successor"})
	if err != nil || startedNow {
		t.Fatalf("queue successor = (%v, %t, %v), want queued task", successor, startedNow, err)
	}

	active.Cancel()
	select {
	case <-active.Done():
	case <-time.After(time.Second):
		t.Fatal("canceled task did not publish terminal state")
	}
	if state := active.Snapshot().State; state != TaskStateCanceled {
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
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskDefault
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
		Mode:   ModeTask,
		Policy: policy,
		Runner: func(_ context.Context, _ *Task, request any) (any, error) {
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

	active, startedNow, err := router.startOrQueueTask(route, Request{Route: route.Name, DecodedValue: "active"})
	if err != nil || !startedNow {
		t.Fatalf("start active = (%v, %t, %v), want started task", active, startedNow, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active handler did not start")
	}
	successor, startedNow, err := router.startOrQueueTask(route, Request{Route: route.Name, DecodedValue: "successor"})
	if err != nil || startedNow {
		t.Fatalf("queue successor = (%v, %t, %v), want queued task", successor, startedNow, err)
	}

	select {
	case <-active.Done():
	case <-time.After(time.Second):
		t.Fatal("timed-out task did not publish terminal state")
	}
	if snapshot := active.Snapshot(); snapshot.State != TaskStateFailed || snapshot.Error == nil || snapshot.Error.Code != 504 {
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
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskDefault
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
		Mode:   ModeTask,
		Policy: policy,
		Runner: func(_ context.Context, _ *Task, request any) (any, error) {
			if request == "active" {
				close(started)
				<-release
				return nil, nil
			}
			close(successorStarted)
			return nil, nil
		},
	}

	active, startedNow, err := router.startOrQueueTask(route, Request{Route: route.Name, DecodedValue: "active"})
	if err != nil || !startedNow {
		t.Fatalf("start active = (%v, %t, %v), want started task", active, startedNow, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active handler did not start")
	}
	successor, startedNow, err := router.startOrQueueTask(route, Request{Route: route.Name, DecodedValue: "successor"})
	if err != nil || startedNow {
		t.Fatalf("queue successor = (%v, %t, %v), want queued task", successor, startedNow, err)
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

func TestRouterTaskFastCompleteReturnsTerminalSnapshot(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	router.TaskRunner("test.fast", func(ctx context.Context, task *Task, _ any) (any, error) {
		return map[string]any{"ok": true}, nil
	}, TaskDefault)

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
	var snapshot TaskSnapshot
	if unmarshalErr := json.Unmarshal(result.Data, &snapshot); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(snapshot): %v", unmarshalErr)
	}
	if snapshot.State != TaskStateCompleted {
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

func TestRouterCallWritesOneResultAndClose(t *testing.T) {
	router := NewRouter(NewTaskService())
	router.Call("test.call", func(_ context.Context, request Request) (any, error) {
		if request.DecodedValue != "decoded" {
			t.Fatalf("request = %#v, want decoded", request.DecodedValue)
		}
		return map[string]any{"ok": true}, nil
	})

	server, client := net.Pipe()
	defer client.Close()
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, Request{
			Route:        "test.call",
			DecodedValue: "decoded",
		})
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
	if result.Status != "ok" || string(result.Data) != `{"ok":true}` {
		t.Fatalf("result = %#v, want ok payload", result)
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

func TestRouterTaskTimeoutReturnsFailedSnapshot(t *testing.T) {
	registry := NewTaskService()
	router := NewRouter(registry)
	policy := TaskDefault
	policy.Name = "timeout_test"
	policy.Timeout = 10 * time.Millisecond
	router.TaskRunner("test.timeout", func(ctx context.Context, task *Task, _ any) (any, error) {
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
	var snapshot TaskSnapshot
	if unmarshalErr := json.Unmarshal(result.Data, &snapshot); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(snapshot): %v", unmarshalErr)
	}
	if snapshot.State != TaskStateFailed {
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

func TestRouterRejectsRegisteredTasksNamespace(t *testing.T) {
	router := NewRouter(NewTaskService())
	defer func() {
		if recover() == nil {
			t.Fatal("expected reserved tasks.* registration to panic")
		}
	}()
	router.Call("tasks.get", func(context.Context, Request) (any, error) {
		return nil, nil
	})
}
