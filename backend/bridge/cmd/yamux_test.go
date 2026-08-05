package cmd

import (
	"context"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestHandleYamuxSessionStopsAfterFailedStartupHandoff(t *testing.T) {
	rt := runtime.Runtime{Session: &session.Session{SessionID: "test-session"}}
	disconnected := false
	handleYamuxSession(
		context.Background(),
		rt,
		nil,
		nil,
		func() { disconnected = true },
		func() bool { return false },
	)
	if !disconnected {
		t.Fatal("failed startup handoff did not trigger disconnect")
	}
}
