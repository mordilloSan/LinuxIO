package terminal

import (
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// TerminalHandlers exposes terminal control to the bridge IPC.
// Commands:
// - start_main []
// - read_main [waitMs]
// - input_main [data]
// - resize_main [cols rows]
// - close_main []
// - list_shells [containerID]
// - start_container [containerID shell]
// - read_container [containerID waitMs]
// - input_container [containerID data]
// - resize_container [containerID cols rows]
// - close_container [containerID]
func TerminalHandlers(sess *session.Session) map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		"start_main": func(_ []string) (any, error) {
			return map[string]bool{"started": true}, StartTerminal(sess)
		},
		"read_main_backlog": func(_ []string) (any, error) {
			data, err := ReadTerminalBacklog(sess.SessionID)
			if err != nil {
				// If no terminal yet, return empty backlog gracefully
				return map[string]any{"data": ""}, nil
			}
			return map[string]any{"data": data}, nil
		},
		"read_main": func(args []string) (any, error) {
			wait := 750
			if len(args) > 0 {
				if v, err := strconv.Atoi(args[0]); err == nil && v >= 0 {
					wait = v
				}
			}
			data, closed, err := ReadTerminal(sess.SessionID, wait)
			if err != nil && data == "" {
				return map[string]any{"data": "", "closed": true}, nil
			}
			return map[string]any{"data": data, "closed": closed}, nil
		},
		"input_main": func(args []string) (any, error) {
			if len(args) == 0 {
				return map[string]bool{"ok": true}, nil
			}
			return map[string]bool{"ok": true}, WriteToTerminal(sess.SessionID, args[0])
		},
		"resize_main": func(args []string) (any, error) {
			if len(args) < 2 {
				return map[string]bool{"ok": true}, nil
			}
			cols, _ := strconv.Atoi(args[0])
			rows, _ := strconv.Atoi(args[1])
			return map[string]bool{"ok": true}, ResizeTerminal(sess.SessionID, cols, rows)
		},
		"close_main": func(_ []string) (any, error) {
			return map[string]bool{"closed": true}, CloseTerminal(sess.SessionID)
		},

		"list_shells": func(args []string) (any, error) {
			if len(args) < 1 {
				return []string{}, nil
			}
			return ListContainerShells(args[0])
		},
		"start_container": func(args []string) (any, error) {
			if len(args) < 2 {
				return map[string]bool{"started": false}, nil
			}
			return map[string]bool{"started": true}, StartContainerTerminal(sess, args[0], args[1])
		},
		"read_container": func(args []string) (any, error) {
			if len(args) < 1 {
				return map[string]any{"data": "", "closed": true}, nil
			}
			wait := 750
			if len(args) > 1 {
				if v, err := strconv.Atoi(args[1]); err == nil && v >= 0 {
					wait = v
				}
			}
			data, closed, err := ReadContainerTerminal(sess.SessionID, args[0], wait)
			if err != nil && data == "" {
				return map[string]any{"data": "", "closed": true}, nil
			}
			return map[string]any{"data": data, "closed": closed}, nil
		},
		"input_container": func(args []string) (any, error) {
			if len(args) < 2 {
				return map[string]bool{"ok": true}, nil
			}
			return map[string]bool{"ok": true}, WriteToContainerTerminal(sess.SessionID, args[0], args[1])
		},
		"resize_container": func(args []string) (any, error) {
			if len(args) < 3 {
				return map[string]bool{"ok": true}, nil
			}
			cols, _ := strconv.Atoi(args[1])
			rows, _ := strconv.Atoi(args[2])
			return map[string]bool{"ok": true}, ResizeContainerTerminal(sess.SessionID, args[0], cols, rows)
		},
		"close_container": func(args []string) (any, error) {
			if len(args) < 1 {
				return map[string]bool{"closed": true}, nil
			}
			return map[string]bool{"closed": true}, CloseContainerTerminal(sess.SessionID, args[0])
		},
	}
}
