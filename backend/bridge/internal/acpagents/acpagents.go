// Package acpagents is the catalogue of Agent Client Protocol agents LinuxIO
// can launch. The assistant relay spawns them, the terminal runs their login
// flows, and the config handler validates the configured agent id against it.
package acpagents

import (
	"sort"
	"sync"
)

// Agent is the command that starts one ACP agent in stdio mode. Commands come
// from the ACP registry (https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json).
type Agent struct {
	Command string
	Args    []string
}

var (
	mu     sync.RWMutex
	agents = map[string]Agent{
		"claude": {Command: "npx", Args: []string{"@agentclientprotocol/claude-agent-acp"}},
		"gemini": {Command: "npx", Args: []string{"@google/gemini-cli", "--acp"}},
		"codex":  {Command: "npx", Args: []string{"@agentclientprotocol/codex-acp"}},
	}
)

// Lookup returns the launch command for a known agent id.
func Lookup(id string) (Agent, bool) {
	mu.RLock()
	defer mu.RUnlock()
	agent, ok := agents[id]
	return agent, ok
}

// IDs lists the known agent ids, sorted.
func IDs() []string {
	mu.RLock()
	defer mu.RUnlock()
	ids := make([]string, 0, len(agents))
	for id := range agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Override registers or replaces an agent for the duration of a test. It is
// exported so the terminal package's tests can point an agent at a script;
// production code never calls it.
func Override(id string, agent Agent) (restore func()) {
	mu.Lock()
	previous, had := agents[id]
	agents[id] = agent
	mu.Unlock()
	return func() {
		mu.Lock()
		if had {
			agents[id] = previous
		} else {
			delete(agents, id)
		}
		mu.Unlock()
	}
}
