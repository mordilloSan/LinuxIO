package dbusclient

import (
	"context"
	"testing"
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
	ctx = requireContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	signals.mu.Lock()
	for sub := range signals.subs {
		sub.closeOnce.Do(func() {
			// Closing the shared connection drops the bus-side match rules, so
			// individual subscriptions intentionally retain a nil closeErr.
			close(sub.ch)
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
