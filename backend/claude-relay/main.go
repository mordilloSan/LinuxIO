// Command linuxio-claude-relay lets the Assistant page show Claude Code's
// plugin panes. claude-agent-acp runs it in place of `claude`
// (CLAUDE_CODE_EXECUTABLE); it runs the real `claude` and copies the
// adapter's stdio unchanged, and carries the pane control requests between
// Claude Code and the bridge over the unix socket the bridge made for the
// chat. It runs as the session user, never as root.
package main

import (
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run(process{
		args:   os.Args[1:],
		env:    os.Environ(),
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		notify: notifySignals,
		exec:   syscall.Exec,
	}))
}

// notifySignals subscribes c to the signals the relay forwards to Claude
// Code. A signal the relay was started with ignored stays ignored, so Claude
// Code inherits it the same way it would without the relay.
func notifySignals(c chan<- os.Signal) {
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			signal.Notify(c, sig)
		}
	}
}
