package modules

import (
	"fmt"
	"sync"
)

// CommandRegistry maintains a whitelist of allowed commands
// Generic handlers can ONLY execute commands registered here
type CommandRegistry struct {
	mu       sync.RWMutex
	commands map[string]*RegisteredCommand // key: "module.name:command"
	dbus     map[string]*RegisteredDbus    // key: "module.name:command"
}

// RegisteredCommand represents a whitelisted shell command
type RegisteredCommand struct {
	ModuleName  string
	CommandName string
	Template    string        // Command template with {{.argN}} or {{.argName}} placeholders
	Timeout     int
	Args        []HandlerArg  // Argument definitions for proper substitution
}

// RegisteredDbus represents a whitelisted DBus call
type RegisteredDbus struct {
	ModuleName  string
	CommandName string
	Bus         string
	Destination string
	Path        string
	Interface   string
	Method      string
	Args        []string
}

var globalRegistry = &CommandRegistry{
	commands: make(map[string]*RegisteredCommand),
	dbus:     make(map[string]*RegisteredDbus),
}

// GetRegistry returns the global command registry
func GetRegistry() *CommandRegistry {
	return globalRegistry
}

// RegisterCommand adds a command to the whitelist
func (r *CommandRegistry) RegisterCommand(moduleName, commandName, template string, timeout int, args []HandlerArg) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s:%s", moduleName, commandName)
	r.commands[key] = &RegisteredCommand{
		ModuleName:  moduleName,
		CommandName: commandName,
		Template:    template,
		Timeout:     timeout,
		Args:        args,
	}
}

// RegisterDbus adds a DBus call to the whitelist
func (r *CommandRegistry) RegisterDbus(moduleName, commandName string, dbus DbusHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s:%s", moduleName, commandName)
	r.dbus[key] = &RegisteredDbus{
		ModuleName:  moduleName,
		CommandName: commandName,
		Bus:         dbus.Bus,
		Destination: dbus.Destination,
		Path:        dbus.Path,
		Interface:   dbus.Interface,
		Method:      dbus.Method,
		Args:        dbus.Args,
	}
}

// GetCommand retrieves a whitelisted command
// Returns nil if not found (command not allowed)
func (r *CommandRegistry) GetCommand(moduleName, commandName string) *RegisteredCommand {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", moduleName, commandName)
	return r.commands[key]
}

// GetDbus retrieves a whitelisted DBus call
// Returns nil if not found (DBus call not allowed)
func (r *CommandRegistry) GetDbus(moduleName, commandName string) *RegisteredDbus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", moduleName, commandName)
	return r.dbus[key]
}

// IsCommandAllowed checks if a command is whitelisted
func (r *CommandRegistry) IsCommandAllowed(moduleName, commandName string) bool {
	return r.GetCommand(moduleName, commandName) != nil
}

// IsDbusAllowed checks if a DBus call is whitelisted
func (r *CommandRegistry) IsDbusAllowed(moduleName, commandName string) bool {
	return r.GetDbus(moduleName, commandName) != nil
}

// ListCommands returns all registered commands for a module
func (r *CommandRegistry) ListCommands(moduleName string) []*RegisteredCommand {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*RegisteredCommand
	for _, cmd := range r.commands {
		if cmd.ModuleName == moduleName {
			result = append(result, cmd)
		}
	}
	return result
}

// ListDbus returns all registered DBus calls for a module
func (r *CommandRegistry) ListDbus(moduleName string) []*RegisteredDbus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*RegisteredDbus
	for _, dbus := range r.dbus {
		if dbus.ModuleName == moduleName {
			result = append(result, dbus)
		}
	}
	return result
}

// Clear removes all commands for a module (used when unloading)
func (r *CommandRegistry) Clear(moduleName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove commands
	for key, cmd := range r.commands {
		if cmd.ModuleName == moduleName {
			delete(r.commands, key)
		}
	}

	// Remove DBus calls
	for key, dbus := range r.dbus {
		if dbus.ModuleName == moduleName {
			delete(r.dbus, key)
		}
	}
}
