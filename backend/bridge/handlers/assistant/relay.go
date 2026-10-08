package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const (
	streamID       = 1
	stderrTailSize = 4096
	killGrace      = 5 * time.Second
)

// signalGroup is a seam so tests can assert that no signal is sent after exit.
var signalGroup = syscall.Kill

// tailBuffer keeps the last stderrTailSize bytes written to it.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > stderrTailSize {
		b.data = b.data[len(b.data)-stderrTailSize:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

type relay struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stream net.Conn
	done   chan struct{}
	once   sync.Once
}

// HandleAssistantSession spawns the requested ACP agent as the session user
// and relays its stdin/stdout over the duplex stream as data frames. The
// first frame is a ready line with the working directory, the last an exit
// line with the exit code and stderr tail.
func HandleAssistantSession(ctx context.Context, rt runtime.Runtime, stream net.Conn, req apischema.AssistantOpenRequest) error {
	sess := rt.Session
	agent, ok := LookupAgent(req.Agent)
	if !ok {
		closeStream(stream)
		return fmt.Errorf("assistant: unknown agent %q", req.Agent)
	}
	home, err := bridgeconfig.Homedir(sess.User.Username)
	if err != nil {
		closeStream(stream)
		return fmt.Errorf("assistant: resolve home directory: %w", err)
	}

	// The agent runs on the PATH the user's interactive login shell produces
	// (nvm-style installs live there), but is exec'd directly rather than
	// through `bash -i`, whose job-control warnings would land in stderr.
	userPath, err := UserPath(ctx, sess.User)
	if err != nil {
		closeStream(stream)
		return fmt.Errorf("assistant: %w", err)
	}
	binary, err := lookPathIn(userPath, agent.Command)
	if err != nil {
		closeStream(stream)
		return fmt.Errorf("assistant: %s: %w", req.Agent, err)
	}
	cmd := exec.CommandContext(ctx, binary, agent.Args...)
	cmd.Dir = home
	cmd.Env = append(os.Environ(),
		"PATH="+userPath,
		"HOME="+home,
		"USER="+sess.User.Username,
		"LOGNAME="+sess.User.Username,
	)
	sysAttr := &syscall.SysProcAttr{Setsid: true}
	if os.Geteuid() == 0 {
		sysAttr.Credential = &syscall.Credential{Uid: sess.User.UID, Gid: sess.User.GID}
	}
	cmd.SysProcAttr = sysAttr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		closeStream(stream)
		return fmt.Errorf("assistant: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		closeStream(stream)
		return fmt.Errorf("assistant: stdout pipe: %w", err)
	}
	stderr := &tailBuffer{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		closeStream(stream)
		return fmt.Errorf("assistant: start %s: %w", req.Agent, err)
	}
	slog.Debug("assistant agent started", "agent", req.Agent, "user", sess.User.Username, "pid", cmd.Process.Pid)

	r := &relay{cmd: cmd, stdin: stdin, stream: stream, done: make(chan struct{})}

	ready, _ := json.Marshal(map[string]string{"linuxio": "ready", "cwd": home})
	if err := r.writeData(append(ready, '\n')); err != nil {
		r.terminate()
		_ = cmd.Wait()
		close(r.done)
		return fmt.Errorf("assistant: write ready line: %w", err)
	}

	go func() {
		select {
		case <-ctx.Done():
			r.terminate()
		case <-r.done:
		}
	}()

	var wg sync.WaitGroup
	wg.Go(func() {
		r.relayStreamToStdin()
		r.terminate()
	})

	r.relayStdoutToStream(stdout)
	code := exitCode(cmd.Wait())
	close(r.done)

	exit, _ := json.Marshal(map[string]any{"linuxio": "exit", "code": code, "stderr": stderr.String()})
	_ = r.writeData(append(exit, '\n'))
	closeStream(stream)
	_ = stream.Close()
	wg.Wait()
	slog.Debug("assistant agent exited", "agent", req.Agent, "code", code)
	return nil
}

func (r *relay) writeData(payload []byte) error {
	return ipc.WriteRelayFrame(r.stream, &ipc.StreamFrame{Opcode: ipc.OpStreamData, StreamID: streamID, Payload: payload})
}

func (r *relay) relayStdoutToStream(stdout io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			if werr := r.writeData(buf[:n]); werr != nil {
				r.terminate()
				_, _ = io.Copy(io.Discard, stdout)
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (r *relay) relayStreamToStdin() {
	for {
		frame, err := ipc.ReadRelayFrame(r.stream)
		if err != nil {
			return
		}
		switch frame.Opcode {
		case ipc.OpStreamData:
			if len(frame.Payload) > 0 {
				if _, werr := r.stdin.Write(frame.Payload); werr != nil {
					return
				}
			}
		case ipc.OpStreamClose:
			return
		}
	}
}

// terminate hangs up the agent's process group once. SIGHUP first so the
// adapter can clean up; SIGKILL after a grace period if it is still around.
func (r *relay) terminate() {
	// The agent was already reaped; its pid (and process group) may be recycled.
	select {
	case <-r.done:
		return
	default:
	}
	r.once.Do(func() {
		_ = r.stdin.Close()
		if r.cmd.Process == nil {
			return
		}
		pid := r.cmd.Process.Pid
		if err := signalGroup(-pid, syscall.SIGHUP); err != nil {
			slog.Debug("assistant: SIGHUP process group", "error", err)
		}
		time.AfterFunc(killGrace, func() {
			select {
			case <-r.done:
			default:
				_ = signalGroup(-pid, syscall.SIGKILL)
			}
		})
	})
}

func closeStream(stream net.Conn) {
	if err := ipc.WriteStreamClose(stream, streamID); err != nil {
		slog.Debug("assistant: write stream close", "error", err)
	}
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		return exitErr.ExitCode()
	default:
		return -1
	}
}
