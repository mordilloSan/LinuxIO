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
	"github.com/mordilloSan/go_logger/v2/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

var systemBus *godbus.Conn
var sessionBus *godbus.Conn

func init() {
	systemBus, _ = godbus.ConnectSystemBus()
	sessionBus, _ = godbus.ConnectSessionBus()
}

func DbusHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		// NOTE: Direct DBus calls are DISABLED for security
		// DBus calls must be defined in module YAML files
		// Use CallDbusMethodDirect() from module loader instead
		"call": disabledDbusHandler,
	}
}

// disabledDbusHandler returns an error explaining that direct DBus calls are disabled
func disabledDbusHandler(args []string) (any, error) {
	return nil, fmt.Errorf("direct DBus calls are disabled - calls must be defined in module YAML files")
}

// CallDbusMethodDirect executes a DBus call directly (used by module loader)
// This bypasses security checks and should only be called by whitelisted module handlers
func CallDbusMethodDirect(args []string) (any, error) {
	if len(args) < 5 {
		return nil, fmt.Errorf("dbus call requires: bus, destination, path, interface, method")
	}

	busType := args[0]
	destination := args[1]
	path := args[2]
	iface := args[3]
	method := args[4]
	methodArgs := args[5:]

	// Select bus
	var conn *godbus.Conn
	if busType == "session" {
		conn = sessionBus
	} else {
		conn = systemBus
	}

	if conn == nil {
		return nil, fmt.Errorf("failed to connect to %s bus", busType)
	}

	// Get object
	obj := conn.Object(destination, godbus.ObjectPath(path))

	// Convert string args to interface{} for DBus
	dbusArgs := make([]interface{}, len(methodArgs))
	for i, arg := range methodArgs {
		dbusArgs[i] = arg
	}

	// Call method
	call := obj.Call(iface+"."+method, 0, dbusArgs...)
	if call.Err != nil {
		return nil, call.Err
	}

	// Return result (if any)
	if len(call.Body) > 0 {
		return call.Body[0], nil
	}

	return nil, nil
}

// DbusSignalData represents a D-Bus signal forwarded to the client
type DbusSignalData struct {
	SignalName string        `json:"signal_name"`
	Body       []interface{} `json:"body"`
}

// HandleDbusStream handles D-Bus operations with signal streaming.
// This allows modules to call D-Bus methods that emit progress signals.
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

	if len(args) < 5 {
		errMsg := "dbus stream requires at least [bus, destination, path, interface, method]"
		_ = ipc.WriteResultError(stream, 0, errMsg, 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	busType := args[0]
	destination := args[1]
	path := args[2]
	iface := args[3]
	method := args[4]

	// Find signal names and method arguments (separated by "--")
	var signalNames []string
	var methodArgs []string
	separatorIdx := -1

	for i := 5; i < len(args); i++ {
		if args[i] == "--" {
			separatorIdx = i
			break
		}
		signalNames = append(signalNames, args[i])
	}

	if separatorIdx >= 0 && separatorIdx+1 < len(args) {
		methodArgs = args[separatorIdx+1:]
	}

	logger.Infof("[DbusStream] Calling %s.%s on %s (signals: %v)", iface, method, destination, signalNames)

	// Select bus
	var conn *godbus.Conn
	var err error
	if busType == "session" {
		conn, err = godbus.ConnectSessionBus()
	} else {
		conn, err = godbus.ConnectSystemBus()
	}
	if err != nil {
		errMsg := fmt.Sprintf("failed to connect to %s bus: %v", busType, err)
		_ = ipc.WriteResultError(stream, 0, errMsg, 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("[DbusStream] Failed to close D-Bus connection: %v", cerr)
		}
	}()

	// Get object
	obj := conn.Object(destination, godbus.ObjectPath(path))

	// Subscribe to signals
	sigCh := make(chan *godbus.Signal, 100)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	// Add match for the object path
	if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(godbus.ObjectPath(path))); err != nil {
		logger.Warnf("[DbusStream] Failed to add D-Bus match signal: %v", err)
	}

	// Convert method args to interface{}
	dbusArgs := make([]interface{}, len(methodArgs))
	for i, arg := range methodArgs {
		dbusArgs[i] = arg
	}

	// Call the D-Bus method
	logger.Debugf("[DbusStream] Calling method %s.%s with %d args", iface, method, len(dbusArgs))
	call := obj.Call(iface+"."+method, 0, dbusArgs...)
	if call.Err != nil {
		errMsg := fmt.Sprintf("D-Bus method call failed: %v", call.Err)
		_ = ipc.WriteResultError(stream, 0, errMsg, 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	// Create a map of signal names for quick lookup
	// Store both full names and short names for flexible matching
	signalMap := make(map[string]bool)
	for _, sig := range signalNames {
		fullName := iface + "." + sig
		signalMap[fullName] = true
		signalMap[sig] = true // Also store short name for fallback matching
	}

	// Process signals until timeout or specific signal received
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	hasFinishedSignal := signalMap[iface+".Finished"] || signalMap[iface+".Done"] || signalMap[iface+".Complete"]

	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				continue
			}

			// Check if this is a signal we care about
			if !signalMap[sig.Name] {
				// Also check without interface prefix (for flexibility)
				shortName := strings.TrimPrefix(sig.Name, iface+".")
				if !signalMap[shortName] {
					continue
				}
			}

			logger.Debugf("[DbusStream] Received signal: %s", sig.Name)

			// Send signal to client as JSON
			signalData := DbusSignalData{
				SignalName: sig.Name,
				Body:       sig.Body,
			}
			payload, err := json.Marshal(signalData)
			if err != nil {
				logger.Warnf("[DbusStream] Failed to marshal signal: %v", err)
				continue
			}

			_ = ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 0,
				Payload:  payload,
			})

			// If this is a "Finished" signal, we're done
			if hasFinishedSignal && (sig.Name == iface+".Finished" || sig.Name == iface+".Done" || sig.Name == iface+".Complete") {
				logger.Infof("[DbusStream] Received Finished signal, operation complete")
				_ = ipc.WriteResultOK(stream, 0, map[string]interface{}{"completed": true})
				_ = ipc.WriteStreamClose(stream, 0)
				return nil
			}

		case <-ctx.Done():
			// Timeout - but this might be OK if there's no explicit Finished signal
			if !hasFinishedSignal {
				logger.Infof("[DbusStream] Context timeout, operation assumed complete")
				_ = ipc.WriteResultOK(stream, 0, map[string]interface{}{"completed": true})
			} else {
				logger.Warnf("[DbusStream] Timeout waiting for signals")
				_ = ipc.WriteResultError(stream, 0, "timeout waiting for D-Bus signals", 500)
			}
			_ = ipc.WriteStreamClose(stream, 0)
			return ctx.Err()
		}
	}
}
