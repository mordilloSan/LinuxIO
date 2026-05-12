package dbusclient

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient/testdbus"
)

func TestRetryOnceIfClosedRunsOnceOnSuccess(t *testing.T) {
	t.Cleanup(setRetryDelayForTest(0))

	var calls int
	err := RetryOnceIfClosed(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("RetryOnceIfClosed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRetryOnceIfClosedRetriesClosedConnection(t *testing.T) {
	t.Cleanup(setRetryDelayForTest(0))

	var calls int
	err := RetryOnceIfClosed(context.Background(), func() error {
		calls++
		if calls == 1 {
			return net.ErrClosed
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RetryOnceIfClosed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestRetryOnceIfClosedHonorsCanceledContextBeforeRetry(t *testing.T) {
	t.Cleanup(setRetryDelayForTest(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	err := RetryOnceIfClosed(ctx, func() error {
		calls++
		cancel()
		return net.ErrClosed
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRetryOnceIfClosedDoesNotRetryOtherErrors(t *testing.T) {
	t.Cleanup(setRetryDelayForTest(0))

	want := errors.New("boom")
	var calls int
	err := RetryOnceIfClosed(context.Background(), func() error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestUseSystemBusWithOptionsNoRetryDisablesClosedRetry(t *testing.T) {
	t.Cleanup(setRetryDelayForTest(0))

	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	var calls int
	err := UseSystemBusWithOptions(context.Background(), SystemBusOptions{
		Unserialized: true,
		NoRetry:      true,
	}, func(context.Context, *godbus.Conn) error {
		calls++
		return net.ErrClosed
	})
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("err = %v, want net.ErrClosed", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestSystemBusLockSerializesCalls(t *testing.T) {
	t.Cleanup(setRetryDelayForTest(0))

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	var secondEntered atomic.Bool

	go func() {
		firstDone <- withSystemBusLock(context.Background(), func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()

	<-firstEntered
	go func() {
		secondDone <- withSystemBusLock(context.Background(), func() error {
			secondEntered.Store(true)
			return nil
		})
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second call finished before first released: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if secondEntered.Load() {
		t.Fatalf("second call entered while first call held the lock")
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !secondEntered.Load() {
		t.Fatalf("second call did not enter after first released")
	}
}

func TestSystemBusLockHonorsCanceledContext(t *testing.T) {
	t.Cleanup(setRetryDelayForTest(0))

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		firstDone <- withSystemBusLock(context.Background(), func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	<-firstEntered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := withSystemBusLock(ctx, func() error {
		t.Fatalf("lock body ran for a canceled context")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first call: %v", err)
	}
}

type echoService struct {
	mu    sync.Mutex
	calls []string
}

func (s *echoService) Echo(value string) (string, *godbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, value)
	return value, nil
}

func (s *echoService) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

type propertiesService struct {
	values map[string]godbus.Variant
}

func (s propertiesService) Get(iface, property string) (godbus.Variant, *godbus.Error) {
	value, ok := s.values[iface+"."+property]
	if !ok {
		return godbus.Variant{}, godbus.MakeFailedError(errors.New("unknown property"))
	}
	return value, nil
}

func TestSystemObjectUseRoutesToConfiguredObjectAndClosesConnection(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	service := &echoService{}
	owner := bus.OwnName(t, "org.example.Test")
	if err := owner.Export(service, godbus.ObjectPath("/org/example/Test"), "org.example.Test"); err != nil {
		t.Fatalf("export service: %v", err)
	}

	var (
		got      string
		usedConn *godbus.Conn
	)
	err := SystemObject{
		Subsystem: "test",
		BusName:   "org.example.Test",
		Path:      godbus.ObjectPath("/org/example/Test"),
	}.Use(context.Background(), func(_ context.Context, conn *godbus.Conn, obj godbus.BusObject) error {
		usedConn = conn
		if !conn.Connected() {
			t.Fatalf("connection was closed inside callback")
		}
		return obj.Call("org.example.Test.Echo", 0, "pong").Store(&got)
	})
	if err != nil {
		t.Fatalf("SystemObject.Use: %v", err)
	}
	if got != "pong" {
		t.Fatalf("got %q, want pong", got)
	}
	if calls := service.Calls(); len(calls) != 1 || calls[0] != "pong" {
		t.Fatalf("service calls = %#v, want [pong]", calls)
	}
	if usedConn == nil {
		t.Fatalf("callback did not receive a connection")
	}
	if usedConn.Connected() {
		t.Fatalf("connection remained open after callback")
	}
}

func TestSystemInterfaceCallStore(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	service := &echoService{}
	owner := bus.OwnName(t, "org.example.Test")
	if err := owner.Export(service, godbus.ObjectPath("/org/example/Test"), "org.example.Test"); err != nil {
		t.Fatalf("export service: %v", err)
	}

	iface := SystemObject{
		Subsystem: "test",
		BusName:   "org.example.Test",
		Path:      godbus.ObjectPath("/org/example/Test"),
	}.Interface("org.example.Test")

	var got string
	if err := iface.CallStore(context.Background(), "Echo", CallPolicy{}, []any{"pong"}, &got); err != nil {
		t.Fatalf("CallStore: %v", err)
	}
	if got != "pong" {
		t.Fatalf("got %q, want pong", got)
	}
}

func TestSystemObjectUseSession(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	service := &echoService{}
	owner := bus.OwnName(t, "org.example.Test")
	if err := owner.Export(service, godbus.ObjectPath("/org/example/Test"), "org.example.Test"); err != nil {
		t.Fatalf("export service: %v", err)
	}
	if err := owner.Export(service, godbus.ObjectPath("/org/example/Other"), "org.example.Test"); err != nil {
		t.Fatalf("export other service: %v", err)
	}

	obj := SystemObject{
		Subsystem: "test",
		BusName:   "org.example.Test",
		Path:      godbus.ObjectPath("/org/example/Test"),
	}

	var got string
	var other string
	err := obj.UseSession(context.Background(), func(session SystemSession) error {
		if session.Context() == nil {
			t.Fatalf("session context is nil")
		}
		if err := session.CallStore("org.example.Test.Echo", CallPolicy{}, []any{"pong"}, &got); err != nil {
			return err
		}
		return session.ObjectAt(godbus.ObjectPath("/org/example/Other")).
			CallWithContext(session.Context(), "org.example.Test.Echo", 0, "other").
			Store(&other)
	})
	if err != nil {
		t.Fatalf("UseSession: %v", err)
	}
	if got != "pong" {
		t.Fatalf("got %q, want pong", got)
	}
	if other != "other" {
		t.Fatalf("other = %q, want other", other)
	}
}

func TestReadBusNameStateReportsActiveName(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	owner := bus.OwnName(t, "org.example.Available")
	state, err := ReadBusNameState(context.Background(), owner, "org.example.Available")
	if err != nil {
		t.Fatalf("ReadBusNameState: %v", err)
	}
	if !state.Active {
		t.Fatalf("state.Active = false, want true")
	}
	if !state.Available() {
		t.Fatalf("state.Available() = false, want true")
	}

	missing, err := ReadBusNameState(context.Background(), owner, "org.example.Missing")
	if err != nil {
		t.Fatalf("ReadBusNameState missing name: %v", err)
	}
	if missing.Available() {
		t.Fatalf("missing state = %#v, want unavailable", missing)
	}
}

func TestSystemObjectAvailableUsesSystemBus(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	bus.OwnName(t, "org.example.Available")
	ok, err := (SystemObject{
		Subsystem: "test",
		BusName:   "org.example.Available",
		Path:      godbus.ObjectPath("/org/example/Available"),
	}).Available(context.Background())
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if !ok {
		t.Fatalf("Available = false, want true")
	}
}

func TestBusNameAvailableBypassesSystemBusGate(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	bus.OwnName(t, "org.example.Available")

	gateHeld := make(chan struct{})
	releaseGate := make(chan struct{})
	gateDone := make(chan error, 1)
	go func() {
		gateDone <- withSystemBusLock(context.Background(), func() error {
			close(gateHeld)
			<-releaseGate
			return nil
		})
	}()
	<-gateHeld
	defer func() {
		close(releaseGate)
		if err := <-gateDone; err != nil {
			t.Fatalf("gate holder: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ok, err := BusNameAvailable(ctx, "org.example.Available")
	if err != nil {
		t.Fatalf("BusNameAvailable: %v", err)
	}
	if !ok {
		t.Fatalf("BusNameAvailable = false, want true")
	}
}

func TestReadBusNameStateRejectsInvalidInput(t *testing.T) {
	if _, err := ReadBusNameState(context.Background(), nil, "org.example.Missing"); err == nil {
		t.Fatalf("ReadBusNameState with nil connection succeeded")
	}

	bus := testdbus.Start(t)
	conn := bus.Connect(t)
	if _, err := ReadBusNameState(context.Background(), conn, " "); err == nil {
		t.Fatalf("ReadBusNameState with empty name succeeded")
	}
}

func TestSystemObjectRequireAvailableOnConnection(t *testing.T) {
	bus := testdbus.Start(t)
	conn := bus.Connect(t)

	want := errors.New("missing service")
	err := (SystemObject{
		Subsystem:   "test",
		BusName:     "org.example.Missing",
		Path:        godbus.ObjectPath("/org/example/Missing"),
		Unavailable: want,
	}).RequireAvailableOnConnection(context.Background(), conn)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestSystemSessionRequireAvailableUsesSessionConnection(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	bus.OwnName(t, "org.example.Available")

	obj := SystemObject{
		Subsystem: "test",
		BusName:   "org.example.Available",
		Path:      godbus.ObjectPath("/org/example/Available"),
	}
	err := obj.UseSessionWithOptions(context.Background(), SystemBusOptions{
		Unserialized: true,
	}, func(session SystemSession) error {
		return session.RequireAvailable()
	})
	if err != nil {
		t.Fatalf("RequireAvailable: %v", err)
	}
}

func TestGetInterfaceProperty(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	owner := bus.OwnName(t, "org.example.Test")
	props := propertiesService{
		values: map[string]godbus.Variant{
			"org.example.Test.Title": godbus.MakeVariant("hello"),
		},
	}
	if err := owner.Export(props, godbus.ObjectPath("/org/example/Test"), PropertiesIface); err != nil {
		t.Fatalf("export properties: %v", err)
	}

	iface := SystemObject{
		Subsystem: "test",
		BusName:   "org.example.Test",
		Path:      godbus.ObjectPath("/org/example/Test"),
	}.Interface("org.example.Test")

	got, err := GetInterfaceProperty[string](context.Background(), iface, "Title")
	if err != nil {
		t.Fatalf("GetInterfaceProperty: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q, want hello", got)
	}
}

func TestWatchSignalsReceivesAndClosesMatch(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	t.Cleanup(func() {
		if err := CloseSignals(context.Background()); err != nil {
			t.Fatalf("close signals: %v", err)
		}
	})

	sender := bus.OwnName(t, "org.example.Signals")

	ctx := context.Background()
	sub, err := WatchSignals(
		ctx,
		1,
		SignalMatch{
			Interface: "org.example.Signals",
			Member:    "Changed",
			Path:      godbus.ObjectPath("/org/example/Signals"),
		})
	if err != nil {
		t.Fatalf("WatchSignals: %v", err)
	}
	defer func() {
		if err := sub.Close(context.Background()); err != nil {
			t.Fatalf("close subscription: %v", err)
		}
	}()

	if err := sender.Emit(godbus.ObjectPath("/org/example/Signals"), "org.example.Signals.Changed", "value"); err != nil {
		t.Fatalf("emit signal: %v", err)
	}

	select {
	case sig := <-sub.Chan():
		if sig.Name != "org.example.Signals.Changed" {
			t.Fatalf("signal name = %q, want org.example.Signals.Changed", sig.Name)
		}
		if !reflect.DeepEqual(sig.Body, []any{"value"}) {
			t.Fatalf("signal body = %#v, want [value]", sig.Body)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for signal")
	}
}

func TestSignalsNotStarvedByGate(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	t.Cleanup(func() {
		if err := CloseSignals(context.Background()); err != nil {
			t.Fatalf("close signals: %v", err)
		}
	})

	sender := bus.OwnName(t, "org.example.Signals")
	gateHeld := make(chan struct{})
	releaseGate := make(chan struct{})
	gateDone := make(chan error, 1)
	go func() {
		gateDone <- withSystemBusLock(context.Background(), func() error {
			close(gateHeld)
			<-releaseGate
			return nil
		})
	}()
	<-gateHeld
	defer func() {
		close(releaseGate)
		if err := <-gateDone; err != nil {
			t.Fatalf("gate holder: %v", err)
		}
	}()

	sub, err := WatchSignals(context.Background(), 1, SignalMatch{
		Interface: "org.example.Signals",
		Member:    "Changed",
		Path:      godbus.ObjectPath("/org/example/Signals"),
	})
	if err != nil {
		t.Fatalf("WatchSignals: %v", err)
	}
	defer sub.Close(context.Background())

	if err := sender.Emit(godbus.ObjectPath("/org/example/Signals"), "org.example.Signals.Changed", "value"); err != nil {
		t.Fatalf("emit signal: %v", err)
	}

	select {
	case sig := <-sub.Chan():
		if sig == nil {
			t.Fatalf("subscription closed while gate was held")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for signal while gate was held")
	}
}

func TestSignalSubscriptionsFilterLocally(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	t.Cleanup(func() {
		if err := CloseSignals(context.Background()); err != nil {
			t.Fatalf("close signals: %v", err)
		}
	})

	sender := bus.OwnName(t, "org.example.Signals")
	changed, err := WatchSignals(context.Background(), 1, SignalMatch{
		Interface: "org.example.Signals",
		Member:    "Changed",
		Path:      godbus.ObjectPath("/org/example/Signals"),
	})
	if err != nil {
		t.Fatalf("watch changed: %v", err)
	}
	defer changed.Close(context.Background())

	other, err := WatchSignals(context.Background(), 1, SignalMatch{
		Interface: "org.example.Signals",
		Member:    "Other",
		Path:      godbus.ObjectPath("/org/example/Signals"),
	})
	if err != nil {
		t.Fatalf("watch other: %v", err)
	}
	defer other.Close(context.Background())

	if err := sender.Emit(godbus.ObjectPath("/org/example/Signals"), "org.example.Signals.Changed", "value"); err != nil {
		t.Fatalf("emit signal: %v", err)
	}

	select {
	case sig := <-changed.Chan():
		if sig == nil || sig.Name != "org.example.Signals.Changed" {
			t.Fatalf("changed subscription received %#v", sig)
		}
	case <-time.After(time.Second):
		t.Fatalf("changed subscription did not receive signal")
	}

	select {
	case sig := <-other.Chan():
		t.Fatalf("other subscription received unrelated signal %#v", sig)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestIdenticalSignalSubscriptionsRefCountMatch(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	t.Cleanup(func() {
		if err := CloseSignals(context.Background()); err != nil {
			t.Fatalf("close signals: %v", err)
		}
	})

	sender := bus.OwnName(t, "org.example.Signals")
	match := SignalMatch{
		Interface: "org.example.Signals",
		Member:    "Changed",
		Path:      godbus.ObjectPath("/org/example/Signals"),
	}
	first, err := WatchSignals(context.Background(), 1, match)
	if err != nil {
		t.Fatalf("watch first: %v", err)
	}
	second, err := WatchSignals(context.Background(), 1, match)
	if err != nil {
		t.Fatalf("watch second: %v", err)
	}
	defer second.Close(context.Background())

	if err := sender.Emit(godbus.ObjectPath("/org/example/Signals"), "org.example.Signals.Changed", "one"); err != nil {
		t.Fatalf("emit first signal: %v", err)
	}
	assertSignalBody(t, first.Chan(), "org.example.Signals.Changed", "one")
	assertSignalBody(t, second.Chan(), "org.example.Signals.Changed", "one")

	if err := first.Close(context.Background()); err != nil {
		t.Fatalf("close first: %v", err)
	}
	if _, ok := <-first.Chan(); ok {
		t.Fatalf("first subscription channel stayed open after Close")
	}

	if err := sender.Emit(godbus.ObjectPath("/org/example/Signals"), "org.example.Signals.Changed", "two"); err != nil {
		t.Fatalf("emit second signal: %v", err)
	}
	assertSignalBody(t, second.Chan(), "org.example.Signals.Changed", "two")
}

func TestCloseSignalsClosesSubscriptionsAndAllowsReopen(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	t.Cleanup(func() {
		if err := CloseSignals(context.Background()); err != nil {
			t.Fatalf("close signals: %v", err)
		}
	})

	sender := bus.OwnName(t, "org.example.Signals")
	match := SignalMatch{
		Interface: "org.example.Signals",
		Member:    "Changed",
		Path:      godbus.ObjectPath("/org/example/Signals"),
	}
	sub, err := WatchSignals(context.Background(), 1, match)
	if err != nil {
		t.Fatalf("WatchSignals: %v", err)
	}
	if signalErr := CloseSignals(context.Background()); signalErr != nil {
		t.Fatalf("CloseSignals: %v", signalErr)
	}
	if _, ok := <-sub.Chan(); ok {
		t.Fatalf("subscription channel stayed open after CloseSignals")
	}

	reopened, err := WatchSignals(context.Background(), 1, match)
	if err != nil {
		t.Fatalf("WatchSignals after CloseSignals: %v", err)
	}
	defer reopened.Close(context.Background())

	if err := sender.Emit(godbus.ObjectPath("/org/example/Signals"), "org.example.Signals.Changed", "reopened"); err != nil {
		t.Fatalf("emit signal: %v", err)
	}
	assertSignalBody(t, reopened.Chan(), "org.example.Signals.Changed", "reopened")
}

func TestKnownObjectsAndCallPolicy(t *testing.T) {
	if SystemdManager.BusName != SystemdBusName || SystemdManager.Path != godbus.ObjectPath(SystemdPath) {
		t.Fatalf("SystemdManager = %#v", SystemdManager)
	}
	if PackageKit.Interface(PackageKitIface).Method("CreateTransaction") != PackageKitIface+".CreateTransaction" {
		t.Fatalf("unexpected packagekit method name")
	}
	if PowerProfiles.BusName != PowerProfilesBusName || PowerProfiles.Path != godbus.ObjectPath(PowerProfilesPath) {
		t.Fatalf("PowerProfiles = %#v", PowerProfiles)
	}

	flags := CallPolicy{
		NoAutoStart:                   true,
		AllowInteractiveAuthorization: true,
		NoReplyExpected:               true,
	}.Flags()
	for _, flag := range []godbus.Flags{
		godbus.FlagNoAutoStart,
		godbus.FlagAllowInteractiveAuthorization,
		godbus.FlagNoReplyExpected,
	} {
		if flags&flag == 0 {
			t.Fatalf("flags %v missing %v", flags, flag)
		}
	}
}

func assertSignalBody(t *testing.T, ch <-chan *godbus.Signal, wantName, wantBody string) {
	t.Helper()

	select {
	case sig, ok := <-ch:
		if !ok {
			t.Fatalf("subscription channel closed, want signal %q", wantName)
		}
		if sig.Name != wantName {
			t.Fatalf("signal name = %q, want %q", sig.Name, wantName)
		}
		if len(sig.Body) != 1 || sig.Body[0] != wantBody {
			t.Fatalf("signal body = %#v, want [%q]", sig.Body, wantBody)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for signal %q", wantName)
	}
}

func setRetryDelayForTest(delay time.Duration) func() {
	previous := retryDelay
	retryDelay = delay
	return func() {
		retryDelay = previous
	}
}
