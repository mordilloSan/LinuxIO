package pcpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type noopEvents struct{}

func (noopEvents) Data([]byte) error      { return nil }
func (noopEvents) Progress(any) error     { return nil }
func (noopEvents) Result(any) error       { return nil }
func (noopEvents) Error(error, int) error { return nil }
func (noopEvents) Close(string) error     { return nil }

func TestRegisterHandlersRequiresPrivilege(t *testing.T) {
	ipc.UnregisterAll("pcp_api")
	t.Cleanup(func() {
		ipc.UnregisterAll("pcp_api")
	})

	RegisterHandlers(&session.Session{Privileged: false})

	handler, ok := ipc.Get("pcp_api", "get_config")
	if !ok {
		t.Fatal("expected get_config handler to be registered")
	}

	err := handler.Execute(context.Background(), nil, noopEvents{})
	if err == nil {
		t.Fatal("expected privilege error")
	}
	if !strings.Contains(err.Error(), "administrator privileges") {
		t.Fatalf("expected privilege error, got %v", err)
	}
}
