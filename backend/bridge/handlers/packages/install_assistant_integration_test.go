package packages

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/usercmd"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// TestAssistantAgentIntegration installs every catalogued agent with the real
// npm from the registry, into a temporary home, and starts each one far
// enough to answer ACP `initialize`. It proves what unit tests cannot: that
// the pinned versions exist, that their executables are where the catalogue
// says, and that Claude's adapter runs without its own copy of Claude Code.
// It needs Node.js, network access and, for Claude, `claude` on the login-shell
// PATH, so it only runs through `make test-assistant-agent-integration`.
func TestAssistantAgentIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_ASSISTANT_AGENT_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_ASSISTANT_AGENT_INTEGRATION=1 (make test-assistant-agent-integration) to run")
	}
	sessionUser := currentSessionUser(t)

	home := t.TempDir()
	originalHome := capabilityUserHome
	capabilityUserHome = func(string) (string, error) { return home, nil }
	t.Cleanup(func() { capabilityUserHome = originalHome })

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	userPath, err := usercmd.Path(ctx, sessionUser)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range acpagents.IDs() {
		t.Run(id, func(t *testing.T) {
			agent, _ := acpagents.Lookup(id)
			if err := installAssistantAgent(ctx, nil, sessionUser, id); err != nil {
				t.Fatalf("install %s: %v", agent.Spec(), err)
			}
			if err := acpagents.Installed(id, home); err != nil {
				t.Fatalf("installed check: %v", err)
			}
			size := treeSize(t, acpagents.Dir(home, id))
			t.Logf("%s installed in %d MB", agent.Spec(), size>>20)
			if agent.CLI != "" && size > 150<<20 {
				t.Fatalf("%s takes %d MB: its bundled CLI was installed although it runs the user's own", agent.Spec(), size>>20)
			}

			launch, err := acpagents.Launch(id, home, userPath)
			if err != nil {
				t.Fatalf("launch: %v", err)
			}
			answer := initializeAgent(ctx, t, sessionUser, launch, userPath)
			if answer.Result.ProtocolVersion == 0 {
				t.Fatalf("initialize answered without a protocol version: %+v", answer)
			}
			t.Logf("%s answered initialize as %s %s", id, answer.Result.AgentInfo.Name, answer.Result.AgentInfo.Version)
		})
	}
}

func currentSessionUser(t *testing.T) session.User {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.ParseUint(current.Uid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.ParseUint(current.Gid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	return session.User{Username: current.Username, UID: uint32(uid), GID: uint32(gid)}
}

type initializeAnswer struct {
	ID     int `json:"id"`
	Result struct {
		ProtocolVersion int `json:"protocolVersion"`
		AgentInfo       struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"agentInfo"`
	} `json:"result"`
}

// initializeAgent starts the agent the way the assistant relay does and
// returns its answer to ACP `initialize`.
func initializeAgent(ctx context.Context, t *testing.T, user session.User, launch acpagents.Command, userPath string) initializeAnswer {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd, err := usercmd.Command(ctx, user, launch.Path, launch.Args...)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(cmd.Env, "PATH="+userPath)
	cmd.Env = append(cmd.Env, launch.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", launch.Path, err)
	}
	defer func() {
		_ = stdin.Close()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()

	const request = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"auth":{"terminal":true}}}}` + "\n"
	if _, err := stdin.Write([]byte(request)); err != nil {
		t.Fatalf("write initialize: %v", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for scanner.Scan() {
		var answer initializeAnswer
		if json.Unmarshal(scanner.Bytes(), &answer) == nil && answer.ID == 1 {
			return answer
		}
	}
	t.Fatalf("%s exited or timed out before answering initialize (scan error: %v)", launch.Path, scanner.Err())
	return initializeAnswer{}
}

func treeSize(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}
