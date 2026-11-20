package dbusclient

import (
	"context"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"
)

func TestSplitSignalName(t *testing.T) {
	tests := []struct {
		name       string
		wantIface  string
		wantMember string
	}{
		{name: "org.freedesktop.DBus.NameOwnerChanged", wantIface: "org.freedesktop.DBus", wantMember: "NameOwnerChanged"},
		{name: "NameOwnerChanged", wantMember: "NameOwnerChanged"},
		{name: ".Changed", wantMember: "Changed"},
		{name: "org.example.", wantIface: "org.example"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface, member := splitSignalName(tt.name)
			if iface != tt.wantIface || member != tt.wantMember {
				t.Fatalf("splitSignalName(%q) = (%q, %q), want (%q, %q)", tt.name, iface, member, tt.wantIface, tt.wantMember)
			}
		})
	}
}

func closeSignalsForTest(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	signals.mu.Lock()
	for sub := range signals.subs {
		sub.closeOnce.Do(func() {
			// Closing the shared connection drops the bus-side match rules, so
			// individual subscriptions intentionally retain a nil closeErr.
			sub.stop()
		})
	}
	signals.subs = nil
	signals.matchRefs = nil
	signals.raw = nil

	if signals.conn == nil {
		signals.mu.Unlock()
		return nil
	}

	conn := signals.conn
	signals.conn = nil
	signals.mu.Unlock()
	return conn.Close()
}

// A reader that is behind still gets every signal, in order: a burst longer
// than the buffer used to lose the signals past it, and with them the
// Finished that ends a PackageKit transaction.
func TestSignalSubscriptionKeepsEverySignalForASlowReader(t *testing.T) {
	match := SignalMatch{Interface: "org.example.Items", Path: "/org/example/Items"}
	sub := newSignalSubscription(1, match.key(), match)
	m := &signalManager{subs: map[*SignalSubscription]struct{}{sub: {}}}

	const items = 500
	for i := range items {
		m.dispatchSignal(&godbus.Signal{Path: match.Path, Name: "org.example.Items.Item", Body: []any{i}})
	}
	m.dispatchSignal(&godbus.Signal{Path: match.Path, Name: "org.example.Items.Finished"})
	// Not this subscription's: another path.
	m.dispatchSignal(&godbus.Signal{Path: "/org/example/Other", Name: "org.example.Items.Item"})

	for i := range items {
		select {
		case sig := <-sub.Chan():
			if sig.Name != "org.example.Items.Item" || sig.Body[0] != i {
				t.Fatalf("signal %d = %s %v, want item %d", i, sig.Name, sig.Body, i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("signal %d never arrived", i)
		}
	}
	select {
	case sig := <-sub.Chan():
		if sig.Name != "org.example.Items.Finished" {
			t.Fatalf("after the items came %s, want Finished", sig.Name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Finished never arrived")
	}

	sub.closeOnce.Do(sub.stop)
	select {
	case sig, ok := <-sub.Chan():
		if ok {
			t.Fatalf("a signal of another path arrived: %s %s", sig.Path, sig.Name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the channel stayed open after the subscription ended")
	}
}

// A subscriber that stopped reading holds neither the dispatcher nor its own
// close, and its channel still closes.
func TestSignalSubscriptionWithNoReaderEndsWhenStopped(t *testing.T) {
	match := SignalMatch{Interface: "org.example.Items", Path: "/org/example/Items"}
	sub := newSignalSubscription(1, match.key(), match)
	m := &signalManager{subs: map[*SignalSubscription]struct{}{sub: {}}}
	for range 100 {
		m.dispatchSignal(&godbus.Signal{Path: match.Path, Name: "org.example.Items.Item"})
	}

	sub.closeOnce.Do(sub.stop)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-sub.Chan():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the channel stayed open after the subscription ended")
		}
	}
}
