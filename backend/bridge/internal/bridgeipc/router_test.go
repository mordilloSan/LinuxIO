package bridgeipc

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func dispatchResult(t *testing.T, router *Router, req Request) (relay.ResultFrame, error) {
	t.Helper()
	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- router.Dispatch(context.Background(), server, req)
	}()

	frame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if frame.Opcode != relay.OpStreamResult {
		t.Fatalf("result opcode = 0x%02x, want OpStreamResult", frame.Opcode)
	}
	var result relay.ResultFrame
	if unmarshalErr := json.Unmarshal(frame.Payload, &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(result): %v", unmarshalErr)
	}
	closeFrame, err := relay.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if closeFrame.Opcode != relay.OpStreamClose {
		t.Fatalf("close opcode = 0x%02x, want OpStreamClose", closeFrame.Opcode)
	}
	return result, <-errCh
}

func TestStatusCodeMapsNotFoundAndConflict(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{ErrNotFound, 404},
		{fmt.Errorf("%w: source path", ErrNotFound), 404},
		{ErrConflict, 409},
		{fmt.Errorf("%w: destination already exists", ErrConflict), 409},
		{fmt.Errorf("%w: %w", ErrInvalidArgs, errors.New("bad name")), 400},
	}
	for _, tt := range tests {
		if got := StatusCode(tt.err); got != tt.want {
			t.Errorf("StatusCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestFailureLevel(t *testing.T) {
	tests := []struct {
		err  error
		want slog.Level
	}{
		{ErrInvalidArgs, slog.LevelWarn},
		{ErrNotFound, slog.LevelWarn},
		{NewError("update failed", 1), slog.LevelError},
		{NewError("boom", 500), slog.LevelError},
		{errors.New("plain"), slog.LevelError},
	}
	for _, tt := range tests {
		if got := failureLevel(tt.err); got != tt.want {
			t.Errorf("failureLevel(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// testJobID returns the nth distinct job ID.
func testJobID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// registeredMutation registers a Mutation route and returns it as the router
// keeps it.
func registeredMutation(t *testing.T, router *Router, name string, runner TaskRunner, opts ...RouteOption) Route {
	t.Helper()
	router.Mutation(name, runner, opts...)
	route, ok := router.lookup(name)
	if !ok {
		t.Fatalf("mutation route %s was not registered", name)
	}
	return route
}

// startJob starts route's task for req as job id, as dispatchMutation does.
func startJob(router *Router, route Route, req Request, id string) (*Task, bool, error) {
	return router.startStableTask(route, req, mutationIdentity(route.Name, id, req.RawRequest))
}

func TestRouterExclusiveAdmissionIsAtomic(t *testing.T) {
	router := NewRouter(NewTaskService())
	release := make(chan struct{})
	route := registeredMutation(t, router, "test.atomic.singleton", func(context.Context, *Task, any) (any, error) {
		<-release
		return nil, nil
	}, Exclusive)

	const callers = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, callers)
	for i := range callers {
		wg.Go(func() {
			<-start
			_, _, err := startJob(router, route, Request{Route: route.Name}, testJobID(i))
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
			t.Fatalf("start error = %v, want ErrDuplicateActive", err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d exclusive tasks, want 1", successes)
	}
	close(release)
}

func TestRouterConcurrentStableIdentityStartsOnlyOnce(t *testing.T) {
	router := NewRouter(NewTaskService())
	release := make(chan struct{})
	runnerStarted := make(chan struct{})
	var runs atomic.Int32
	route := registeredMutation(t, router, "control.app_update", func(context.Context, *Task, any) (any, error) {
		runs.Add(1)
		close(runnerStarted)
		<-release
		return nil, nil
	}, Exclusive)
	request := Request{
		Route:      route.Name,
		RawRequest: jsontext.Value(`"same-request"`),
		Owner:      TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000},
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
			task, _, err := startJob(router, route, request, testJobID(42))
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

func TestRouterDoesNotLabelUnknownClientRoute(t *testing.T) {
	const credential = "feedfacefeedfacefeedfacefeedface"
	router := NewRouter(NewTaskService())
	server, client := net.Pipe()
	defer client.Close()

	dispatched := make(chan struct{})
	release := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		err := router.Dispatch(context.Background(), server, Request{Route: credential})
		close(dispatched)
		<-release
		errCh <- err
	}()

	if _, err := relay.ReadRelayFrame(client); err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if _, err := relay.ReadRelayFrame(client); err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	<-dispatched

	var dump bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&dump, 2); err != nil {
		close(release)
		t.Fatalf("dump goroutines: %v", err)
	}
	if strings.Contains(dump.String(), credential) {
		close(release)
		t.Fatalf("traceback contains client-controlled unknown route:\n%s", dump.String())
	}
	close(release)
	if err := <-errCh; !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("Dispatch error = %v, want ErrRouteNotFound", err)
	}
}

func TestRouterLabelsRegisteredRoute(t *testing.T) {
	const routeName = "test.labeled.route"
	router := NewRouter(NewTaskService())
	gotRoute := make(chan string, 1)
	router.Call(routeName, func(ctx context.Context, _ Request) (any, error) {
		pprof.ForLabels(ctx, func(key, value string) bool {
			if key == "route" {
				gotRoute <- value
			}
			return true
		})
		return nil, nil
	})

	if result, err := dispatchResult(t, router, Request{Route: routeName}); err != nil || result.Status != "ok" {
		t.Fatalf("Dispatch = %#v, %v; want success", result, err)
	}
	select {
	case got := <-gotRoute:
		if got != routeName {
			t.Fatalf("route label = %q, want %q", got, routeName)
		}
	default:
		t.Fatal("registered handler context has no route label")
	}
}

// waitForRouterSettle polls until the router no longer counts any task of a
// route as running, or fails the test.
func waitForRouterSettle(t *testing.T, router *Router, routeName string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		router.registry.admitMu.Lock()
		activeCount := router.registry.activeByRoute[routeName]
		targets := len(router.registry.activeTargets)
		router.registry.admitMu.Unlock()
		if activeCount == 0 && targets == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("router accounting did not settle: active=%d targets=%d", activeCount, targets)
		}
		time.Sleep(time.Millisecond)
	}
}

// targetRoute runs a task per request string until release closes, naming
// the request as its target.
func targetRoute(t *testing.T, router *Router, name string, release <-chan struct{}, running *atomic.Int32) Route {
	return registeredMutation(t, router, name, func(context.Context, *Task, any) (any, error) {
		running.Add(1)
		defer running.Add(-1)
		<-release // Deliberately ignore cancellation.
		return nil, nil
	}, WithTaskMetadata(func(request any) TaskMetadata {
		target, _ := request.(string)
		return TaskMetadata{Identity: []string{target}}
	}))
}

func TestRouterRunsTasksOnDifferentTargetsSideBySide(t *testing.T) {
	router := NewRouter(NewTaskService())
	release := make(chan struct{})
	var running atomic.Int32
	route := targetRoute(t, router, "test.side-by-side", release, &running)
	owner := TaskOwner{SessionID: "session-a", Username: "alice", UID: 1000}

	// One user copies six folders at once.
	var tasks []*Task
	for i := range 6 {
		task, started, err := startJob(router, route, Request{Route: route.Name, DecodedValue: fmt.Sprintf("folder-%d", i), Owner: owner}, testJobID(i))
		if err != nil || !started {
			t.Fatalf("start %d = %t, %v; want started", i, started, err)
		}
		tasks = append(tasks, task)
	}
	deadline := time.Now().Add(time.Second)
	for running.Load() != 6 {
		if time.Now().After(deadline) {
			t.Fatalf("running = %d, want 6 side by side", running.Load())
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	for _, task := range tasks {
		<-task.Done()
	}
	waitForRouterSettle(t, router, route.Name)
}

func TestRouterRefusesASecondTaskOnTheSameTargetUntilItsHandlerExits(t *testing.T) {
	router := NewRouter(NewTaskService())
	release := make(chan struct{})
	var running atomic.Int32
	route := targetRoute(t, router, "test.same-target", release, &running)
	poolA := Request{Route: route.Name, DecodedValue: "pool-a"}

	first, _, err := startJob(router, route, poolA, testJobID(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = startJob(router, route, poolA, testJobID(2)); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("second start on pool-a = %v, want ErrDuplicateActive", err)
	}
	if StatusCode(fmt.Errorf("%w: x", ErrDuplicateActive)) != 409 {
		t.Fatal("a refused duplicate must map to 409")
	}

	// A canceled task turns terminal at once, but its handler still runs, so
	// the target stays taken until the handler exits.
	first.Cancel()
	<-first.Done()
	if state := first.Snapshot().State; state != TaskStateCanceled {
		t.Fatalf("first state = %q, want canceled", state)
	}
	if _, _, err = startJob(router, route, poolA, testJobID(3)); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("start while the canceled handler runs = %v, want ErrDuplicateActive", err)
	}
	close(release)
	waitForRouterSettle(t, router, route.Name)
	again, started, err := startJob(router, route, poolA, testJobID(4))
	if err != nil || !started {
		t.Fatalf("start after the handler exited = %t, %v", started, err)
	}
	<-again.Done()
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
