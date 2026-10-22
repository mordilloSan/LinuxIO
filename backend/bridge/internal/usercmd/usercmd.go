// Package usercmd builds commands that run as the session user inside that
// user's login environment. The terminal, assistant and package handlers all
// spawn through it so the credential switch and PATH resolution exist once.
package usercmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const probeTimeout = 5 * time.Second

// LoginShell is bash when it is installed, otherwise sh.
func LoginShell() string {
	if _, err := exec.LookPath("bash"); err != nil {
		return "sh"
	}
	return "bash"
}

// LoginShellArgv runs command through the user's interactive login shell so
// `npx` resolves on the user's own PATH, as it would in the Terminal page.
// Interactive matters: ~/.bashrc returns early for non-interactive shells,
// and that is where nvm-style version managers add themselves. The command
// and arguments are passed positionally to `exec "$0" "$@"`; nothing is
// interpolated into the shell script. Only used where a PTY is attached;
// pipe-based spawns use Path instead to keep bash's job-control warnings out
// of the child's stderr.
func LoginShellArgv(command string, args, extra []string) []string {
	argv := []string{LoginShell(), "-i", "-l", "-c", `exec "$0" "$@"`, command}
	argv = append(argv, args...)
	return append(argv, slices.Clone(extra)...)
}

// Path returns the PATH the user's interactive login shell ends up with,
// evaluated as that user. Stdin is /dev/null and stderr is discarded, so the
// "no job control" warnings bash prints without a tty never reach a caller.
func Path(ctx context.Context, u session.User) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd, err := Command(ctx, u, LoginShell(), "-i", "-l", "-c", `printf %s "$PATH"`)
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

// Command builds a command that runs as u in u's home directory, in its own
// session, with HOME, USER and LOGNAME set for u. Credentials are only
// switched when the bridge runs as root.
func Command(ctx context.Context, u session.User, name string, args ...string) (*exec.Cmd, error) {
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

// LookPathIn is exec.LookPath against an explicit PATH value. A command that
// contains a slash is used as given.
func LookPathIn(path, command string) (string, error) {
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
// their interactive login shell sees it, which is also how agents are run.
func ProbeLoginShell(ctx context.Context, u session.User, command string) error {
	path, err := Path(ctx, u)
	if err != nil {
		return fmt.Errorf("probe %s: %w", command, err)
	}
	if _, err := LookPathIn(path, command); err != nil {
		return fmt.Errorf("%s not found on %s's login-shell PATH", command, u.Username)
	}
	return nil
}
