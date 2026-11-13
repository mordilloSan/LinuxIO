// Package acpagents is the catalogue of Agent Client Protocol agents LinuxIO
// can launch. The capability installer installs them, the assistant relay
// spawns them, the terminal runs their login flows, and the config handler
// validates the configured agent id against it.
package acpagents

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/usercmd"
)

// Agent is one ACP agent: the npm package that provides it, pinned to the
// version LinuxIO installs and runs, and how to start it in stdio mode.
// Packages and versions come from the ACP registry
// (https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json).
type Agent struct {
	Package string
	Version string
	// Bin is the package's executable under node_modules/.bin.
	Bin  string
	Args []string
	// InstallArgs are extra `npm install` flags for this package.
	InstallArgs []string
	// CLI names a command the agent drives instead of its own bundled copy,
	// and CLIEnv the variable that tells the agent where that command is.
	// Both are empty for a self-contained package.
	CLI    string
	CLIEnv string
	// Panes marks a CLI that serves plugin panes over stream-JSON control
	// requests. The assistant then runs linuxio-claude-relay in its place, so
	// the browser can reach them.
	Panes bool
}

// Spec is the exact `npm install` argument for the agent.
func (a Agent) Spec() string {
	return a.Package + "@" + a.Version
}

// Command is an installed agent, ready to run as the user it was resolved for.
type Command struct {
	Path string
	Args []string
	// Env holds the variables to add to the user's environment.
	Env []string
	// CLI is the resolved path of the agent's CLI, empty for a self-contained
	// package.
	CLI string
}

var (
	mu     sync.RWMutex
	agents = map[string]Agent{
		"claude": {
			Package: "@agentclientprotocol/claude-agent-acp",
			Version: "0.102.0",
			Bin:     "claude-agent-acp",
			// The adapter's own copy of Claude Code is an optional dependency
			// of its SDK; it runs the installed one instead.
			InstallArgs: []string{"--omit=optional"},
			CLI:         "claude",
			CLIEnv:      "CLAUDE_CODE_EXECUTABLE",
			Panes:       true,
		},
		"gemini": {
			Package: "@google/gemini-cli",
			Version: "0.67.0",
			Bin:     "gemini",
			Args:    []string{"--acp"},
		},
		"codex": {
			Package: "@agentclientprotocol/codex-acp",
			Version: "2.6.0",
			Bin:     "codex-acp",
		},
	}
)

// Lookup returns the catalogue entry for a known agent id.
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
// exported so other packages' tests can install a fake agent; production code
// never calls it.
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

// Dir is where LinuxIO installs an agent for the user whose home is home. Each
// user has their own copy because Node.js itself is per user.
func Dir(home, id string) string {
	return filepath.Join(home, ".local", "share", "linuxio", "agents", id)
}

// manifestMaxBytes bounds how much of a package manifest is read; real ones
// are a few kilobytes.
const manifestMaxBytes = 1 << 20

// Installed reports whether the pinned version of the agent is installed
// under home. LinuxIO runs only the version it pins, so another version
// counts as not installed and the error says which one was found.
func Installed(id, home string) error {
	agent, ok := Lookup(id)
	if !ok {
		return fmt.Errorf("unknown agent %q", id)
	}
	root, err := os.OpenRoot(Dir(home, id))
	if err != nil {
		return fmt.Errorf("%s is not installed", agent.Package)
	}
	defer root.Close()
	return checkManifest(root, agent)
}

// checkManifest compares the installed package's version with the pinned one.
// The directory belongs to the user while the bridge reading it may be root,
// so it is read through an os.Root, which follows no link out of the
// directory, and the manifest must be a regular file of ordinary size: a
// planted FIFO or device cannot stall or flood the caller.
func checkManifest(root *os.Root, agent Agent) error {
	notInstalled := fmt.Errorf("%s is not installed", agent.Package)
	name := filepath.Join("node_modules", filepath.FromSlash(agent.Package), "package.json")
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return notInstalled
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > manifestMaxBytes {
		return notInstalled
	}
	data, err := io.ReadAll(io.LimitReader(file, manifestMaxBytes))
	if err != nil {
		return notInstalled
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil || pkg.Version == "" {
		return notInstalled
	}
	if pkg.Version != agent.Version {
		return fmt.Errorf("%s %s is installed, LinuxIO needs %s", agent.Package, pkg.Version, agent.Version)
	}
	return nil
}

// Launch resolves how to start an installed agent for one user: home is the
// user's home directory and path their login-shell PATH. Nothing is
// downloaded; an agent that is not installed is an error.
func Launch(id, home, path string) (Command, error) {
	agent, ok := Lookup(id)
	if !ok {
		return Command{}, fmt.Errorf("unknown agent %q", id)
	}
	dir := Dir(home, id)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Command{}, fmt.Errorf("%s is not installed", agent.Package)
	}
	defer root.Close()
	if err = checkManifest(root, agent); err != nil {
		return Command{}, err
	}
	// npm links the bin to a file inside the package; a link that leaves the
	// agent's directory is not one npm made.
	bin := filepath.Join("node_modules", ".bin", agent.Bin)
	info, err := root.Stat(bin)
	if err != nil {
		return Command{}, fmt.Errorf("%s: %w", agent.Package, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return Command{}, fmt.Errorf("%s: %s is not executable", agent.Package, bin)
	}
	command := Command{Path: filepath.Join(dir, bin), Args: agent.Args}
	if agent.CLI != "" {
		cli, lookErr := usercmd.LookPathIn(path, agent.CLI)
		if lookErr != nil {
			return Command{}, lookErr
		}
		command.Env = []string{agent.CLIEnv + "=" + cli}
		command.CLI = cli
	}
	return command, nil
}
