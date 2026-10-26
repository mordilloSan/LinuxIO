package packages

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func useTempAptTermLog(t *testing.T, initial string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "term.log")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("write term.log: %v", err)
	}
	previous := aptTermLogPath
	aptTermLogPath = path
	t.Cleanup(func() { aptTermLogPath = previous })
	writer, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open term.log: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	return writer
}

func TestAptTermLogRelaysOnlyNewCompleteTerminalLines(t *testing.T) {
	writer := useTempAptTermLog(t, "Setting up earlier (1.0) ...\n")
	termLog := openAptTermLog()
	if termLog == nil {
		t.Fatal("openAptTermLog returned nil for a readable log")
	}
	defer termLog.Close()

	write := func(text string) {
		t.Helper()
		if _, err := writer.WriteString(text); err != nil {
			t.Fatalf("append term.log: %v", err)
		}
	}
	write("Log started: 2026-10-24  16:25:21\n" +
		"(Reading database ... 5%\r(Reading database ... 142073 files and directories currently installed.)\n" +
		"Unpacking gh (2.103.2) over (2.103.0) ...\n" +
		"Setting up g")
	if got, want := termLog.lines(), []string{
		"(Reading database ... 142073 files and directories currently installed.)",
		"Unpacking gh (2.103.2) over (2.103.0) ...",
	}; !slices.Equal(got, want) {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}

	write("h (2.103.2) ...\nLog ended: 2026-10-24  16:25:21\n\n")
	if got, want := termLog.lines(), []string{"Setting up gh (2.103.2) ..."}; !slices.Equal(got, want) {
		t.Fatalf("lines = %#v, want the completed partial line %#v", got, want)
	}
	if got := termLog.lines(); got != nil {
		t.Fatalf("lines = %#v, want nil when nothing was appended", got)
	}
}

func TestAptTermLogKeepsLinesEndingInCRLF(t *testing.T) {
	writer := useTempAptTermLog(t, "")
	termLog := openAptTermLog()
	if termLog == nil {
		t.Fatal("openAptTermLog returned nil for a readable log")
	}
	defer termLog.Close()

	if _, err := writer.WriteString("Unpacking gh (2.103.2) ...\r\n(Reading database ... 5%\r(Reading database ... 100%\r\n"); err != nil {
		t.Fatalf("append term.log: %v", err)
	}
	if got, want := termLog.lines(), []string{"Unpacking gh (2.103.2) ...", "(Reading database ... 100%"}; !slices.Equal(got, want) {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}

func TestOpenAptTermLogWithoutLogDisablesOutput(t *testing.T) {
	previous := aptTermLogPath
	aptTermLogPath = filepath.Join(t.TempDir(), "missing.log")
	t.Cleanup(func() { aptTermLogPath = previous })

	termLog := openAptTermLog()
	if termLog != nil {
		t.Fatalf("openAptTermLog = %#v, want nil", termLog)
	}
	termLog.Close()
	if got := termLog.lines(); got != nil {
		t.Fatalf("lines = %#v, want nil", got)
	}
}

func TestPackageUpdateOutputIsTransient(t *testing.T) {
	registry := bridgeipc.NewTaskService()
	task, _, err := registry.ClaimForOwnerWithIdentity("packages.update", nil, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := taskPkgUpdateReporter(task)(&PkgUpdateProgress{Type: "output", Message: "Setting up gh (2.103.2) ..."}); err != nil {
		t.Fatalf("report: %v", err)
	}

	if progress := task.Snapshot().Progress; progress != nil {
		t.Fatalf("snapshot progress = %#v, want output kept out of durable task state", progress)
	}
	_, replay, unsubscribe := task.SubscribeWithReplay(1)
	defer unsubscribe()
	if len(replay) != 1 {
		t.Fatalf("replay = %#v, want the output frame for direct watchers", replay)
	}
	progress, ok := replay[0].Progress.(bridgeipc.TaskProgress)
	if !ok || progress.Message != "Setting up gh (2.103.2) ..." {
		t.Fatalf("replayed progress = %#v, want the dpkg line", replay[0].Progress)
	}
}

func TestCapabilityInstallReporterForwardsDpkgOutputAsStdout(t *testing.T) {
	registry := bridgeipc.NewTaskService()
	task, _, err := registry.ClaimForOwnerWithIdentity("system.install_capability", nil, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	reporter := capabilityInstallReporter(task, "samba", pctInstallStart, pctInstallEnd)
	if err := reporter(&PkgUpdateProgress{Type: "output", Message: "Setting up samba (4.20) ..."}); err != nil {
		t.Fatalf("reporter: %v", err)
	}

	_, replay, unsubscribe := task.SubscribeWithReplay(1)
	defer unsubscribe()
	if len(replay) != 1 {
		t.Fatalf("replay = %#v, want one output record", replay)
	}
	progress, ok := replay[0].Progress.(bridgeipc.TaskProgress)
	if !ok {
		t.Fatalf("replayed progress = %T, want TaskProgress", replay[0].Progress)
	}
	detail, ok := progress.Detail.(InstallCapabilityProgress)
	if !ok || detail.Output == nil || detail.Output.Stream != "stdout" || detail.Output.Text != "Setting up samba (4.20) ...\n" {
		t.Fatalf("detail = %#v, want a stdout record", progress.Detail)
	}
	if !strings.HasPrefix(detail.Message, "Installing samba") {
		t.Fatalf("message = %q, want the install header kept", detail.Message)
	}
}
