package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// LinuxIO's own package repository on a dnf host, as the file an
// administrator installs names it (packaging/scripts/repo-rpm.sh).
const ownRepositoryID = "linuxio"

// ownRepositoryReasonMax bounds the reason handed to the page, in bytes.
const ownRepositoryReasonMax = 600

var (
	ownRepositoryFile = "/etc/yum.repos.d/linuxio.repo"
	// ownRepositoryCheckTimeout bounds the check. The repository's metadata
	// is a few kilobytes; dnf itself waits two minutes on a server that
	// accepts no connection, which is longer than a refresh should hang.
	ownRepositoryCheckTimeout = time.Minute
	ownRepositoryLookPath     = hostcmd.LookPath
)

// checkOwnRepository answers with the reason when LinuxIO's own repository
// cannot be read, dnf's own or that dnf gave no answer in time, and with ""
// when it can be read, when the host has none, and when the check itself
// could not be made.
//
// Fedora sets skip_if_unavailable for every repository that is not its own,
// so dnf, and PackageKit with it, goes on without a repository whose
// metadata it cannot fetch or whose signature fails: a refresh succeeds, no
// LinuxIO update is listed, and nothing says why. LinuxIO's repository file
// leaves that setting alone, because turning it off there fails every dnf
// transaction on the host while the repository is unreachable. This asks dnf
// about that one repository with the setting off for this one command.
// PackageKit's apt backend fails the refresh itself, so an apt host needs no
// such check. See docs/packaging.md, "LinuxIO's own update".
//
// --assumeyes accepts the repository's key, the one its root-owned file
// names, where dnf has not held it yet: PackageKit keeps its own, so without
// it a host whose LinuxIO was never installed by dnf would be told that a
// repository PackageKit reads is unreadable.
func checkOwnRepository(parent context.Context) string {
	if _, err := os.Stat(ownRepositoryFile); err != nil {
		return ""
	}
	dnf, err := ownRepositoryLookPath("dnf")
	if err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(parent, ownRepositoryCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, dnf,
		"--repo="+ownRepositoryID,
		"--setopt="+ownRepositoryID+".skip_if_unavailable=False",
		"--refresh",
		"--assumeyes",
		"makecache",
	)
	// dnf's own words reach the page, so they are not translated.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.WaitDelay = 5 * time.Second
	output, err := cmd.CombinedOutput()
	if err == nil {
		return ""
	}
	var exit *exec.ExitError
	switch {
	case parent.Err() != nil:
		// The request ended, and nothing was learned.
		slog.Debug("own package repository check cancelled", "repository", ownRepositoryID, "error", err)
		return ""
	case ctx.Err() != nil:
		// A repository that gives no answer cannot be read either.
		return fmt.Sprintf("dnf did not answer within %s", ownRepositoryCheckTimeout)
	case !errors.As(err, &exit) || !exit.Exited():
		// Only a dnf that ran to its end speaks about the repository.
		slog.Warn("own package repository check did not run", "repository", ownRepositoryID, "error", err)
		return ""
	}
	if reason := lastLine(output); reason != "" {
		return reason
	}
	return exit.Error()
}

// lastLine is the last line of output that holds text, trimmed and cut to
// ownRepositoryReasonMax bytes on a character.
func lastLine(output []byte) string {
	lines := bytes.Split(output, []byte("\n"))
	for _, line := range slices.Backward(lines) {
		line := strings.TrimSpace(string(line))
		if line == "" {
			continue
		}
		if len(line) <= ownRepositoryReasonMax {
			return line
		}
		const ellipsis = "…"
		cut := ownRepositoryReasonMax - len(ellipsis)
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		return line[:cut] + ellipsis
	}
	return ""
}
