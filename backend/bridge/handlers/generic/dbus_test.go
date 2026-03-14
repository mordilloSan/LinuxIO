package generic

import (
	"encoding/json"
	"net"
	"reflect"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/internal/testdbus"
)

type echoService struct{}

func (echoService) Echo(input string) (string, *godbus.Error) {
	return "echo:" + input, nil
}

type streamService struct {
	conn  *godbus.Conn
	path  godbus.ObjectPath
	iface string
}

func (s *streamService) Run(label string) *godbus.Error {
	if err := s.conn.Emit(s.path, s.iface+".Noise", "ignore"); err != nil {
		return godbus.MakeFailedError(err)
	}
	if err := s.conn.Emit(s.path, s.iface+".Progress", label, uint32(25)); err != nil {
		return godbus.MakeFailedError(err)
	}
	if err := s.conn.Emit(s.path, s.iface+".Finished", label); err != nil {
		return godbus.MakeFailedError(err)
	}
	return nil
}

func TestCallDbusMethodDirect(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	const (
		busName = "com.example.Direct"
		iface   = "com.example.Direct"
	)
	path := godbus.ObjectPath("/com/example/Direct")
	conn := bus.OwnName(t, busName)
	if err := conn.Export(echoService{}, path, iface); err != nil {
		t.Fatalf("export echo service: %v", err)
	}

	got, err := CallDbusMethodDirect([]string{"system", busName, string(path), iface, "Echo", "hello"})
	if err != nil {
		t.Fatalf("CallDbusMethodDirect: %v", err)
	}
	if got != "echo:hello" {
		t.Fatalf("CallDbusMethodDirect returned %#v, want %q", got, "echo:hello")
	}
}

func TestHandleDbusStream(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSessionBus(t)

	const (
		busName = "com.example.Stream"
		iface   = "com.example.Stream"
	)
	path := godbus.ObjectPath("/com/example/Stream")
	conn := bus.OwnName(t, busName)
	service := &streamService{
		conn:  conn,
		path:  path,
		iface: iface,
	}
	if err := conn.Export(service, path, iface); err != nil {
		t.Fatalf("export stream service: %v", err)
	}

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		errCh <- HandleDbusStream(serverConn, []string{
			"session",
			busName,
			string(path),
			iface,
			"Run",
			"Progress",
			"Finished",
			"--",
			"job-42",
		})
	}()

	var signals []DbusSignalData
	var result ipc.ResultFrame
	for {
		frame, err := ipc.ReadRelayFrame(clientConn)
		if err != nil {
			t.Fatalf("ReadRelayFrame: %v", err)
		}

		switch frame.Opcode {
		case ipc.OpStreamData:
			var signal DbusSignalData
			if err := json.Unmarshal(frame.Payload, &signal); err != nil {
				t.Fatalf("unmarshal signal payload: %v", err)
			}
			signals = append(signals, signal)
		case ipc.OpStreamResult:
			if err := json.Unmarshal(frame.Payload, &result); err != nil {
				t.Fatalf("unmarshal result payload: %v", err)
			}
		case ipc.OpStreamClose:
			if err := <-errCh; err != nil {
				t.Fatalf("HandleDbusStream: %v", err)
			}

			if result.Status != "ok" {
				t.Fatalf("result status = %q, want ok", result.Status)
			}

			var payload struct {
				Completed bool `json:"completed"`
			}
			if err := json.Unmarshal(result.Data, &payload); err != nil {
				t.Fatalf("unmarshal result data: %v", err)
			}
			if !payload.Completed {
				t.Fatal("result completed = false, want true")
			}

			gotNames := []string{signals[0].SignalName, signals[1].SignalName}
			wantNames := []string{iface + ".Progress", iface + ".Finished"}
			if !reflect.DeepEqual(gotNames, wantNames) {
				t.Fatalf("signal names = %#v, want %#v", gotNames, wantNames)
			}
			if len(signals) != 2 {
				t.Fatalf("signal count = %d, want 2", len(signals))
			}
			return
		}
	}
}
