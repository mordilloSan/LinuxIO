package cmd

import (
	"testing"
)

func TestServeWithSocketActivationErrorHandled(t *testing.T) {
	t.Setenv("LISTEN_PID", "invalid")

	handled, err := serveWithSocketActivation(ServerConfig{}, nil, nil, nil)
	if !handled || err == nil {
		t.Fatalf("got handled=%v err=%v, want handled true and error", handled, err)
	}
}
