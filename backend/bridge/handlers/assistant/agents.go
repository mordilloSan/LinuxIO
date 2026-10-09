// Package assistant spawns an Agent Client Protocol agent as the session
// user and relays its stdio to the browser. The bridge never parses ACP.
package assistant

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const probeTimeout = 5 * time.Second

// Agent is the command that starts one ACP agent in stdio mode. Commands come
// from the ACP registry (https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json).
type Agent struct {
	Command string
	Args    []string
}

var (
	agentsMu sync.RWMutex
	agents   = map[string]Agent{
		"claude": {Command: "npx", Args: []string{"@agentclientprotocol/claude-agent-acp"}},
		"gemini": {Command: "npx", Args: []string{"@google/gemini-cli", "--acp"}},
		"codex":  {Command: "npx", Args: []string{"@agentclientprotocol/codex-acp"}},
	}
)

// LookupAgent returns the launch command for a known agent id.
func LookupAgent(id string) (Agent, bool) {
	agentsMu.RLock()
	defer agentsMu.RUnlock()
	agent, ok := agents[id]
	return agent, ok
}

// AgentIDs lists the known agent ids, sorted.
func AgentIDs() []string {
	agentsMu.RLock()
	defer agentsMu.RUnlock()
	ids := make([]string, 0, len(agents))
	for id := range agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// OverrideAgent registers or replaces an agent for the duration of a test.
// It is exported so the terminal package's tests can point an agent at a
// script; production code never calls it.
func OverrideAgent(id string, agent Agent) (restore func()) {
	agentsMu.Lock()
	previous, had := agents[id]
	agents[id] = agent
	agentsMu.Unlock()
	return func() {
		agentsMu.Lock()
		if had {
			agents[id] = previous
		} else {
			delete(agents, id)
		}
		agentsMu.Unlock()
	}
}

// LoginShellArgv runs the agent through the user's interactive login shell so
// `npx` resolves on the user's own PATH, as it would in the Terminal page.
// Interactive matters: ~/.bashrc returns early for non-interactive shells,
// and that is where nvm-style version managers add themselves. The command
// and arguments are passed positionally to `exec "$0" "$@"`; nothing is
// interpolated into the shell script. Only used where a PTY is attached;
// pipe-based spawns use UserPath instead to keep bash's job-control warnings
// out of the agent's stderr.
func LoginShellArgv(agent Agent, extra []string) []string {
	argv := []string{loginShell(), "-i", "-l", "-c", `exec "$0" "$@"`, agent.Command}
	argv = append(argv, agent.Args...)
	return append(argv, slices.Clone(extra)...)
}

func loginShell() string {
	if _, err := exec.LookPath("bash"); err != nil {
		return "sh"
	}
	return "bash"
}

// UserPath returns the PATH the user's interactive login shell ends up with,
// evaluated as that user. Stdin is /dev/null and stderr is discarded, so the
// "no job control" warnings bash prints without a tty never reach a caller.
func UserPath(ctx context.Context, u session.User) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd, err := UserCommand(ctx, u, loginShell(), "-i", "-l", "-c", `printf %s "$PATH"`)
	if err != nil {
		return "", err
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("read %s's login-shell PATH: %w", u.Username, err)
	}
	path := strings.TrimSpace(stdout.String())
	if path == "" {
		return "", fmt.Errorf("%s's login shell reported an empty PATH", u.Username)
	}
	return path, nil
}

// UserCommand builds a command that runs as u in u's home directory, in its
// own session, with HOME, USER and LOGNAME set for u. Credentials are only
// switched when the bridge runs as root.
func UserCommand(ctx context.Context, u session.User, name string, args ...string) (*exec.Cmd, error) {
	home, err := bridgeconfig.Homedir(u.Username)
	if err != nil {
		return nil, fmt.Errorf("resolve %s's home directory: %w", u.Username, err)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home, "USER="+u.Username, "LOGNAME="+u.Username)
	sysAttr := &syscall.SysProcAttr{Setsid: true}
	if os.Geteuid() == 0 {
		sysAttr.Credential = &syscall.Credential{Uid: u.UID, Gid: u.GID}
	}
	cmd.SysProcAttr = sysAttr
	return cmd, nil
}

// lookPathIn is exec.LookPath against an explicit PATH value. A command that
// contains a slash is used as given.
func lookPathIn(path, command string) (string, error) {
	if strings.Contains(command, "/") {
		if err := isExecutable(command); err != nil {
			return "", err
		}
		return command, nil
	}
	for dir := range strings.SplitSeq(path, ":") {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, command)
		if isExecutable(candidate) == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found on PATH", command)
}

func isExecutable(file string) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", file)
	}
	return nil
}

// ProbeLoginShell reports whether command resolves on the user's own PATH as
// their interactive login shell sees it, which is also how the agent is run.
func ProbeLoginShell(ctx context.Context, u session.User, command string) error {
	path, err := UserPath(ctx, u)
	if err != nil {
		return fmt.Errorf("probe %s: %w", command, err)
	}
	if _, err := lookPathIn(path, command); err != nil {
		return fmt.Errorf("%s not found on %s's login-shell PATH", command, u.Username)
	}
	return nil
}
