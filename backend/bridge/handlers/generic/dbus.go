package generic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func DbusHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		// NOTE: Direct D-Bus calls are disabled for security.
		"call": disabledDbusHandler,
	}
}

// disabledDbusHandler returns an error explaining that direct DBus calls are disabled
func disabledDbusHandler(args []string) (any, error) {
	return nil, fmt.Errorf("direct D-Bus calls are disabled")
}

// DbusSignalData represents a D-Bus signal forwarded to the client
type DbusSignalData struct {
	SignalName string `json:"signal_name"`
	Body       []any  `json:"body"`
}

type dbusStreamRequest struct {
	busType     string
	destination string
	path        string
	iface       string
	method      string
	signalNames []string
	methodArgs  []string
}

// HandleDbusStream handles D-Bus operations with signal streaming.
//
// args format: [bus, destination, path, interface, method, signalName1, signalName2, ..., "--", methodArg1, methodArg2, ...]
// - bus: "system" or "session"
// - destination: D-Bus service name (e.g., "org.example.Service")
// - path: D-Bus object path (e.g., "/org/example/Service")
// - interface: D-Bus interface name (e.g., "org.example.Service")
// - method: D-Bus method to call (e.g., "StartOperation")
// - signalNames: List of signal names to subscribe to (e.g., "Progress", "Finished")
// - "--": Separator between signal names and method arguments
// - methodArgs: Arguments to pass to the D-Bus method
//
// Response: Streams OpStreamData frames with JSON-encoded DbusSignalData, then OpStreamResult
func HandleDbusStream(stream net.Conn, args []string) error {
	logger.Debugf("[DbusStream] Starting with %d args", len(args))

	request, err := parseDbusStreamRequest(args)
	if err != nil {
		return writeDbusStreamError(stream, err.Error(), 400)
	}
	logger.Infof(
		"[DbusStream] Calling %s.%s on %s (signals: %v)",
		request.iface, request.method, request.destination, request.signalNames,
	)

	conn, err := connectDbusStream(request.busType)
	if err != nil {
		return writeDbusStreamError(stream, fmt.Sprintf("failed to connect to %s bus: %v", request.busType, err), 500)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("[DbusStream] Failed to close D-Bus connection: %v", cerr)
		}
	}()

	obj := conn.Object(request.destination, godbus.ObjectPath(request.path))
	sigCh, cleanupSignals := subscribeDbusSignals(conn, request.path)
	defer cleanupSignals()

	if err := callDbusStreamMethod(obj, request); err != nil {
		return writeDbusStreamError(stream, fmt.Sprintf("D-Bus method call failed: %v", err), 500)
	}

	signalMap := buildDbusSignalMap(request.iface, request.signalNames)
	hasFinishedSignal := hasDbusFinishedSignal(signalMap, request.iface)
	return streamDbusSignals(stream, sigCh, request.iface, signalMap, hasFinishedSignal)
}

func parseDbusStreamRequest(args []string) (*dbusStreamRequest, error) {
	if len(args) < 5 {
		return nil, errors.New("dbus stream requires at least [bus, destination, path, interface, method]")
	}
	request := &dbusStreamRequest{
		busType:     args[0],
		destination: args[1],
		path:        args[2],
		iface:       args[3],
		method:      args[4],
	}
	separatorIdx := len(args)
	for i := 5; i < len(args); i++ {
		if args[i] == "--" {
			separatorIdx = i
			break
		}
		request.signalNames = append(request.signalNames, args[i])
	}
	if separatorIdx < len(args)-1 {
		request.methodArgs = append(request.methodArgs, args[separatorIdx+1:]...)
	}
	return request, nil
}

func connectDbusStream(busType string) (*godbus.Conn, error) {
	if busType == "session" {
		return godbus.ConnectSessionBus()
	}
	return godbus.ConnectSystemBus()
}

func subscribeDbusSignals(conn *godbus.Conn, path string) (chan *godbus.Signal, func()) {
	sigCh := make(chan *godbus.Signal, 100)
	conn.Signal(sigCh)
	matchOpt := godbus.WithMatchObjectPath(godbus.ObjectPath(path))
	if err := conn.AddMatchSignal(matchOpt); err != nil {
		logger.Warnf("[DbusStream] Failed to add D-Bus match signal: %v", err)
	}
	return sigCh, func() {
		conn.RemoveSignal(sigCh)
		if err := conn.RemoveMatchSignal(matchOpt); err != nil {
			logger.Debugf("[DbusStream] Failed to remove D-Bus match signal: %v", err)
		}
	}
}

func callDbusStreamMethod(obj godbus.BusObject, request *dbusStreamRequest) error {
	dbusArgs := make([]any, len(request.methodArgs))
	for i, arg := range request.methodArgs {
		dbusArgs[i] = arg
	}
	logger.Debugf("[DbusStream] Calling method %s.%s with %d args", request.iface, request.method, len(dbusArgs))
	return obj.Call(request.iface+"."+request.method, 0, dbusArgs...).Err
}

func buildDbusSignalMap(iface string, signalNames []string) map[string]bool {
	signalMap := make(map[string]bool, len(signalNames)*2)
	for _, sig := range signalNames {
		signalMap[iface+"."+sig] = true
		signalMap[sig] = true
	}
	return signalMap
}

func hasDbusFinishedSignal(signalMap map[string]bool, iface string) bool {
	return signalMap[iface+".Finished"] || signalMap[iface+".Done"] || signalMap[iface+".Complete"]
}

func streamDbusSignals(
	stream net.Conn,
	sigCh <-chan *godbus.Signal,
	iface string,
	signalMap map[string]bool,
	hasFinishedSignal bool,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				continue
			}
			if !matchesDbusSignal(signalMap, iface, sig.Name) {
				continue
			}
			logger.Debugf("[DbusStream] Received signal: %s", sig.Name)
			if err := writeDbusSignalFrame(stream, sig); err != nil {
				return err
			}
			if hasFinishedSignal && isDbusFinishedSignal(iface, sig.Name) {
				logger.Infof("[DbusStream] Received Finished signal, operation complete")
				return writeDbusStreamOK(stream)
			}
		case <-ctx.Done():
			if !hasFinishedSignal {
				logger.Infof("[DbusStream] Context timeout, operation assumed complete")
				return writeDbusStreamOK(stream)
			}
			logger.Warnf("[DbusStream] Timeout waiting for signals")
			return writeDbusStreamError(stream, "timeout waiting for D-Bus signals", 500)
		}
	}
}

func matchesDbusSignal(signalMap map[string]bool, iface, signalName string) bool {
	if signalMap[signalName] {
		return true
	}
	shortName := strings.TrimPrefix(signalName, iface+".")
	return signalMap[shortName]
}

func writeDbusSignalFrame(stream net.Conn, sig *godbus.Signal) error {
	payload, err := json.Marshal(DbusSignalData{
		SignalName: sig.Name,
		Body:       sig.Body,
	})
	if err != nil {
		logger.Warnf("[DbusStream] Failed to marshal signal: %v", err)
		return nil
	}
	if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
		Opcode:   ipc.OpStreamData,
		StreamID: 0,
		Payload:  payload,
	}); err != nil {
		logger.Debugf("[DbusStream] failed to write data frame: %v", err)
		return err
	}
	return nil
}

func isDbusFinishedSignal(iface, signalName string) bool {
	return signalName == iface+".Finished" || signalName == iface+".Done" || signalName == iface+".Complete"
}

func writeDbusStreamOK(stream net.Conn) error {
	if err := ipc.WriteResultOKAndClose(stream, 0, map[string]any{"completed": true}); err != nil {
		logger.Debugf("[DbusStream] failed to write ok+close frame: %v", err)
	}
	return nil
}

func writeDbusStreamError(stream net.Conn, message string, code int) error {
	if err := ipc.WriteResultErrorAndClose(stream, 0, message, code); err != nil {
		logger.Debugf("[DbusStream] failed to write error+close frame: %v", err)
	}
	return errors.New(message)
}
