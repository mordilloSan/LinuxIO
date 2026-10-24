package ws

import (
	"errors"
	"io"
	"net"
	"os"
	"testing"

	"github.com/gorilla/websocket"
	yamux "github.com/libp2p/go-yamux/v5"
)

func TestExpectedCloseErrorsAreMatchedByIdentity(t *testing.T) {
	expected := []error{
		&net.OpError{Op: "read", Err: net.ErrClosed},
		&net.OpError{Op: "read", Err: os.ErrDeadlineExceeded},
		&websocket.CloseError{Code: websocket.CloseGoingAway},
	}
	for _, err := range expected {
		if !isExpectedWSClose(err) {
			t.Errorf("isExpectedWSClose(%v) = false", err)
		}
	}
	if isExpectedWSClose(errors.New("read tcp: connection reset by peer")) {
		t.Error("isExpectedWSClose matched an unrelated error")
	}

	for _, err := range []error{io.EOF, yamux.ErrStreamReset, yamux.ErrStreamClosed, &yamux.StreamError{Remote: true}} {
		if !isExpectedStreamReadClose(err) {
			t.Errorf("isExpectedStreamReadClose(%v) = false", err)
		}
	}
	if isExpectedStreamReadClose(nil) || isExpectedStreamReadClose(yamux.ErrTimeout) {
		t.Error("isExpectedStreamReadClose matched nil or a timeout")
	}
}
