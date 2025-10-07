package terminal

import (
	"os"
	"os/exec"
	"sync"
)

// TerminalSession holds state for a running PTY-backed shell.
type TerminalSession struct {
	PTY    *os.File
	Cmd    *exec.Cmd
	Mu     sync.Mutex
	Open   bool
	Buffer []byte
	// Backlog retains a longer scrollback independent of read drains.
	Backlog []byte
	// notify is signaled (non-blocking) when new data is appended to Buffer.
	notify chan struct{}
}

type TerminalKey struct {
	SessionID   string
	Target      string // "main" or "container"
	ContainerID string
}

var (
	sessionsMu   sync.Mutex
	sessions     = make(map[string]*TerminalSession)      // main shell per SessionID
	containerMap = make(map[TerminalKey]*TerminalSession) // container shells per (SessionID, ContainerID)
)

// getOrNil returns the main terminal for the sessionID (nil if missing).
func getOrNil(sessionID string) *TerminalSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	return sessions[sessionID]
}

// setMain stores the main terminal for the sessionID.
func setMain(sessionID string, ts *TerminalSession) {
	sessionsMu.Lock()
	sessions[sessionID] = ts
	sessionsMu.Unlock()
}

// delMain removes the main terminal for the sessionID and returns it (may be nil).
func delMain(sessionID string) *TerminalSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	ts := sessions[sessionID]
	delete(sessions, sessionID)
	return ts
}

// getContainer returns the container terminal for a session/container (nil if missing).
func getContainer(sessionID, containerID string) *TerminalSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	return containerMap[TerminalKey{SessionID: sessionID, Target: "container", ContainerID: containerID}]
}

// setContainer stores the container terminal for a session/container.
func setContainer(sessionID, containerID string, ts *TerminalSession) {
	sessionsMu.Lock()
	containerMap[TerminalKey{SessionID: sessionID, Target: "container", ContainerID: containerID}] = ts
	sessionsMu.Unlock()
}

// delContainer removes the container terminal and returns it (may be nil).
func delContainer(sessionID, containerID string) *TerminalSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	key := TerminalKey{SessionID: sessionID, Target: "container", ContainerID: containerID}
	ts := containerMap[key]
	delete(containerMap, key)
	return ts
}

// appendOutput appends data to the session buffer with a soft cap and notifies readers.
func (ts *TerminalSession) appendOutput(p []byte) {
	ts.Mu.Lock()
	// Cap the buffer to ~16KiB to prevent unbounded growth
	const capSize = 8192 * 2
	if len(ts.Buffer)+len(p) > capSize {
		// keep tail
		ts.Buffer = append(ts.Buffer[(len(ts.Buffer)+len(p))-capSize:], p...)
	} else {
		ts.Buffer = append(ts.Buffer, p...)
	}
	// Maintain a larger rolling backlog (~256KiB) for reconnections.
	const backlogCap = 256 * 1024
	if len(ts.Backlog)+len(p) > backlogCap {
		// keep tail of backlog
		ts.Backlog = append(ts.Backlog[(len(ts.Backlog)+len(p))-backlogCap:], p...)
	} else {
		ts.Backlog = append(ts.Backlog, p...)
	}
	// Non-blocking notify so concurrent writers don't deadlock
	select {
	case ts.notify <- struct{}{}:
	default:
	}
	ts.Mu.Unlock()
}

// snapshotBacklog returns a copy of the current backlog as string.
func (ts *TerminalSession) snapshotBacklog() string {
	ts.Mu.Lock()
	s := string(ts.Backlog)
	ts.Mu.Unlock()
	return s
}
