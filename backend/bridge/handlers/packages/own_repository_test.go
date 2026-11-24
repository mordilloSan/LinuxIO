package packages

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// fakeDnf puts a dnf on PATH that records its arguments in dir/args and then
// runs body, and makes dir/linuxio.repo the repository file.
func fakeDnf(t *testing.T, body string) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "dnf"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	repoFile := filepath.Join(dir, "linuxio.repo")
	if err := os.WriteFile(repoFile, []byte("[linuxio]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := ownRepositoryFile
	ownRepositoryFile = repoFile
	t.Cleanup(func() { ownRepositoryFile = previous })
	return argsFile
}

// The check names LinuxIO's repository alone and turns its skipping off for
// this one command: the repository file, and every other dnf run, stay as
// they are.
func TestCheckOwnRepositoryAsksDnfAboutLinuxIOAlone(t *testing.T) {
	argsFile := fakeDnf(t, "exit 0")

	if got := checkOwnRepository(context.Background()); got != "" {
		t.Fatalf("a repository dnf can read = %q, want no reason", got)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("dnf was not run: %v", err)
	}
	want := "--repo=linuxio\n--setopt=linuxio.skip_if_unavailable=False\n--refresh\n--assumeyes\nmakecache\n"
	if string(args) != want {
		t.Fatalf("dnf arguments = %q, want %q", args, want)
	}
}

func TestCheckOwnRepositoryAnswersWithDnfsReason(t *testing.T) {
	const reason = `Failed to download metadata (baseurl: "https://packages.example/rpm/stable/x86_64") for repository "linuxio": repomd.xml GPG signature verification error: Bad PGP signature`
	fakeDnf(t, "echo 'Updating and loading repositories:'\necho '>>> repomd.xml GPG signature verification error: Bad PGP signature' >&2\necho '"+reason+"' >&2\necho >&2\nexit 1")

	if got := checkOwnRepository(context.Background()); got != reason {
		t.Fatalf("reason = %q, want dnf's last line %q", got, reason)
	}
}

func TestCheckOwnRepositoryBoundsALongReason(t *testing.T) {
	fakeDnf(t, "printf '%2000s\\n' x >&2\nexit 1")

	got := checkOwnRepository(context.Background())
	if got == "" || len(got) > ownRepositoryReasonMax {
		t.Fatalf("reason is %d bytes, want between 1 and %d", len(got), ownRepositoryReasonMax)
	}
}

// A host without LinuxIO's dnf repository file (every apt host, and an RPM
// installed from a file) has nothing to check.
func TestCheckOwnRepositoryWithoutTheRepositoryFile(t *testing.T) {
	argsFile := fakeDnf(t, "exit 1")
	if err := os.Remove(ownRepositoryFile); err != nil {
		t.Fatal(err)
	}

	if got := checkOwnRepository(context.Background()); got != "" {
		t.Fatalf("no repository file = %q, want no reason", got)
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Fatalf("dnf was run without a repository file (stat: %v)", err)
	}
}

// A repository whose server gives no answer is unreadable as well: dnf waits
// two minutes on it, longer than the check does.
func TestCheckOwnRepositoryThatDoesNotFinish(t *testing.T) {
	// Found before fakeDnf leaves PATH with the fake dnf alone.
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep on this host")
	}
	fakeDnf(t, "exec "+sleep+" 30")
	previous := ownRepositoryCheckTimeout
	ownRepositoryCheckTimeout = 200 * time.Millisecond
	t.Cleanup(func() { ownRepositoryCheckTimeout = previous })

	started := time.Now()
	got := checkOwnRepository(context.Background())
	if got != "dnf did not answer within 200ms" {
		t.Fatalf("a check that timed out = %q, want that dnf did not answer", got)
	}
	if took := time.Since(started); took > 10*time.Second {
		t.Fatalf("the check took %v, want it ended at its timeout", took)
	}
}

// A request that ended, and a dnf that was killed, say nothing about the
// repository.
func TestCheckOwnRepositoryThatLearnedNothing(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep on this host")
	}
	t.Run("the request was cancelled", func(t *testing.T) {
		fakeDnf(t, "exec "+sleep+" 30")
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if got := checkOwnRepository(ctx); got != "" {
			t.Fatalf("a cancelled check = %q, want no reason", got)
		}
	})
	t.Run("dnf was killed", func(t *testing.T) {
		fakeDnf(t, "echo 'Updating and loading repositories:'\nkill -KILL $$")
		if got := checkOwnRepository(context.Background()); got != "" {
			t.Fatalf("a killed dnf = %q, want no reason", got)
		}
	})
}

func TestCheckOwnRepositoryWithoutDnf(t *testing.T) {
	fakeDnf(t, "exit 1")
	previous := ownRepositoryLookPath
	ownRepositoryLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { ownRepositoryLookPath = previous })

	if got := checkOwnRepository(context.Background()); got != "" {
		t.Fatalf("no dnf = %q, want no reason", got)
	}
}

// A refresh that went through carries what the check found; one that failed
// is the error it always was, and nothing is checked.
func TestRefreshCarriesTheOwnRepositoryReason(t *testing.T) {
	setupFakePackageKit(t, false)
	fakeDnf(t, "echo 'the repository cannot be read' >&2\nexit 1")

	got, err := handleRefreshUpdateCache(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got.OwnRepositoryError != "the repository cannot be read" {
		t.Fatalf("own repository error = %q, want dnf's reason", got.OwnRepositoryError)
	}
}

func TestRefreshThatFailsChecksNothing(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.setRefreshError("no network")
	argsFile := fakeDnf(t, "exit 1")

	got, err := handleRefreshUpdateCache(context.Background(), apischema.NoRequest{})
	if err == nil || got.OwnRepositoryError != "" {
		t.Fatalf("refresh = %#v, %v; want the refresh's error and no reason", got, err)
	}
	if _, statErr := os.Stat(argsFile); !os.IsNotExist(statErr) {
		t.Fatalf("dnf was run after a failed refresh (stat: %v)", statErr)
	}
}

func TestLastLine(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"\n \n":                  "",
		"one":                    "one",
		"one\ntwo\n\n":           "two",
		"one\r\n  two words  \n": "two words",
	} {
		if got := lastLine([]byte(in)); got != want {
			t.Errorf("lastLine(%q) = %q, want %q", in, got, want)
		}
	}
	if got := lastLine([]byte(strings.Repeat("é", ownRepositoryReasonMax))); len(got) > ownRepositoryReasonMax || !strings.HasSuffix(got, "…") {
		t.Errorf("a long line was not cut on a character with an ellipsis: %d bytes, %q...", len(got), got[:8])
	}
}
