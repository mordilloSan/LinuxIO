package ipc

import (
	"sync"
)

var (
	registry = make(map[string]map[string]Handler)
	mu       sync.RWMutex
)

// Register adds a handler for the given type and command.
// Thread-safe and can be called from init() functions.
//
// Example:
//
//	ipc.Register("storage", "get_drive_info", myHandler)
//
// Panics if handlerType or command is empty.
func Register(handlerType, command string, handler Handler) {
	if handlerType == "" {
		panic("handlerType cannot be empty")
	}
	if command == "" {
		panic("command cannot be empty")
	}
	if handler == nil {
		panic("handler cannot be nil")
	}

	mu.Lock()
	defer mu.Unlock()

	if registry[handlerType] == nil {
		registry[handlerType] = make(map[string]Handler)
	}
	registry[handlerType][command] = handler
}

// RegisterFunc is a convenience method for registering function handlers.
// It wraps the function in a HandlerFunc adapter.
//
// Example:
//
//	ipc.RegisterFunc("system", "get_cpu_info", func(ctx, args, emit) error {
//	    cpuInfo := FetchCPUInfo()
//	    return emit.Result(cpuInfo)
//	})
func RegisterFunc(handlerType, command string, fn HandlerFunc) {
	Register(handlerType, command, fn)
}

// Get retrieves a handler by type and command.
// Returns (nil, false) if not found.
//
// Example:
//
//	handler, ok := ipc.Get("storage", "get_drive_info")
//	if !ok {
//	    return errors.New("handler not found")
//	}
func Get(handlerType, command string) (Handler, bool) {
	mu.RLock()
	defer mu.RUnlock()

	commands, ok := registry[handlerType]
	if !ok {
		return nil, false
	}

	handler, ok := commands[command]
	return handler, ok
}

// Unregister removes a specific handler by type and command.
// Returns true if the handler was found and removed, false otherwise.
func Unregister(handlerType, command string) bool {
	mu.Lock()
	defer mu.Unlock()

	commands, ok := registry[handlerType]
	if !ok {
		return false
	}

	if _, exists := commands[command]; !exists {
		return false
	}

	delete(commands, command)

	// Clean up empty handler type map
	if len(commands) == 0 {
		delete(registry, handlerType)
	}

	return true
}

// UnregisterAll removes all handlers for a given handler type.
// Returns true if any handlers were removed, false if the type didn't exist.
func UnregisterAll(handlerType string) bool {
	mu.Lock()
	defer mu.Unlock()

	if _, ok := registry[handlerType]; !ok {
		return false
	}

	delete(registry, handlerType)
	return true
}

// List returns all registered handler types and their commands.
// Useful for debugging and introspection.
//
// Returns a map: {"system": ["get_cpu_info", "get_drive_info"], ...}
func List() map[string][]string {
	mu.RLock()
	defer mu.RUnlock()

	result := make(map[string][]string)
	for handlerType, commands := range registry {
		commandList := make([]string, 0, len(commands))
		for command := range commands {
			commandList = append(commandList, command)
		}
		result[handlerType] = commandList
	}
	return result
}
