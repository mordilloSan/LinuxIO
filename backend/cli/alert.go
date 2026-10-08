package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

const (
	alertUsage = "Usage: linuxio alert raise|resolve|auto-update [flags]"

	aptLogTailBytes = 256 << 10
)

var (
	aptUnattendedLogPath = "/var/log/unattended-upgrades/unattended-upgrades.log"
	rebootRequiredPath   = "/run/reboot-required"
	lookupEnv            = os.Getenv
)

func runAlert(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, alertUsage)
		return 1
	}
	var err error
	switch args[0] {
	case "raise":
		err = alertRaise(ctx, args[1:])
	case "resolve":
		err = alertResolve(ctx, args[1:])
	case "auto-update":
		err = alertAutoUpdate(ctx, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown alert action: %s\n%s\n", args[0], alertUsage)
		return 1
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "Failed to record alert: %v\n", err)
		if errors.Is(err, fs.ErrPermission) {
			fmt.Fprintln(os.Stderr, "This command requires sudo")
		}
		return 1
	}
	return 0
}

func newAlertFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("linuxio alert "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func alertRaise(ctx context.Context, args []string) error {
	fs := newAlertFlagSet("raise")
	var obs alerts.Observation
	var fromStdin bool
	fs.StringVar(&obs.Source, "source", "", "alert source (required)")
	fs.StringVar(&obs.Key, "key", "", "alert key (required)")
	fs.StringVar(&obs.Title, "title", "", "alert title (required)")
	fs.StringVar(&obs.Severity, "severity", alerts.SeverityInfo, "info|warning|error")
	fs.StringVar(&obs.Message, "message", "", "alert message")
	fs.BoolVar(&fromStdin, "message-stdin", false, "read the message from stdin")
	fs.StringVar(&obs.Link, "link", "", "in-app path, e.g. /updates")
	fs.StringVar(&obs.OccurrenceID, "occurrence", "", "identity of this occurrence")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if obs.Source == "" || obs.Key == "" || obs.Title == "" {
		return errors.New("--source, --key and --title are required")
	}
	if fromStdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, alerts.MaxMessageLen*2))
		if err != nil {
			return fmt.Errorf("read message from stdin: %w", err)
		}
		obs.Message = truncateMessage(strings.TrimSpace(string(data)))
	}
	return alerts.Raise(ctx, obs)
}

func alertResolve(ctx context.Context, args []string) error {
	fs := newAlertFlagSet("resolve")
	var source, key string
	fs.StringVar(&source, "source", "", "alert source (required)")
	fs.StringVar(&key, "key", "", "alert key (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if source == "" || key == "" {
		return errors.New("--source and --key are required")
	}
	return alerts.Resolve(ctx, source, key)
}

type autoUpdateRun struct {
	Upgraded []string
	Errors   []string
}

func validNotifyPolicy(p string) bool {
	return p == "on_failure" || p == "on_change" || p == "always"
}

func alertAutoUpdate(ctx context.Context, args []string) error {
	fs := newAlertFlagSet("auto-update")
	var provider, policy string
	fs.StringVar(&provider, "provider", "", "apt|dnf|mint")
	fs.StringVar(&policy, "policy", "", "on_failure|on_change|always")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !validNotifyPolicy(policy) {
		return fmt.Errorf("invalid --policy %q", policy)
	}
	var run autoUpdateRun
	switch provider {
	case "apt":
		run = readAptUnattendedRun()
	case "dnf", "mint":
		// Only the service result is known for these providers in this slice.
	default:
		return fmt.Errorf("invalid --provider %q", provider)
	}
	_, rebootErr := os.Stat(rebootRequiredPath)
	occurrence := lookupEnv("INVOCATION_ID")
	if occurrence == "" {
		occurrence = time.Now().UTC().Format(time.RFC3339)
	}
	obs, ok := autoUpdateAlert(policy, lookupEnv("SERVICE_RESULT"), run, rebootErr == nil, occurrence)
	if !ok {
		return nil
	}
	return alerts.Raise(ctx, obs)
}

func readAptUnattendedRun() autoUpdateRun {
	f, err := os.Open(aptUnattendedLogPath)
	if err != nil {
		return autoUpdateRun{}
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > aptLogTailBytes {
		_, _ = f.Seek(info.Size()-aptLogTailBytes, io.SeekStart)
	}
	return parseAptUnattendedLog(f)
}

// parseAptUnattendedLog keeps only the block after the last
// "Starting unattended upgrades script" line.
func parseAptUnattendedLog(r io.Reader) autoUpdateRun {
	var run autoUpdateRun
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.Contains(line, "Starting unattended upgrades script"):
			run = autoUpdateRun{}
		case strings.Contains(line, "Packages that were upgraded:"):
			_, list, _ := strings.Cut(line, "Packages that were upgraded:")
			run.Upgraded = append(run.Upgraded, strings.Fields(list)...)
		case strings.Contains(line, " ERROR "):
			_, msg, _ := strings.Cut(line, " ERROR ")
			run.Errors = append(run.Errors, strings.TrimSpace(msg))
		}
	}
	return run
}

func truncateMessage(s string) string {
	if len(s) <= alerts.MaxMessageLen {
		return s
	}
	cut := alerts.MaxMessageLen - len("…")
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// autoUpdateAlert decides whether a run deserves an alert under policy and
// builds it. serviceResult is systemd's $SERVICE_RESULT; empty means success.
func autoUpdateAlert(policy, serviceResult string, run autoUpdateRun, rebootRequired bool, occurrence string) (alerts.Observation, bool) {
	failed := (serviceResult != "" && serviceResult != "success") || len(run.Errors) > 0
	changed := len(run.Upgraded) > 0
	if !(failed || (policy == "on_change" && changed) || policy == "always") {
		return alerts.Observation{}, false
	}
	obs := alerts.Observation{
		Source: "auto-update", Key: "run", Link: "/updates", OccurrenceID: occurrence,
		Severity: alerts.SeverityInfo,
	}
	var parts []string
	switch {
	case failed:
		obs.Severity = alerts.SeverityError
		obs.Title = "Automatic updates failed"
		if serviceResult != "" && serviceResult != "success" {
			parts = append(parts, "Service result: "+serviceResult+".")
		}
		parts = append(parts, run.Errors...)
	case changed:
		obs.Title = fmt.Sprintf("Automatic updates installed %d packages", len(run.Upgraded))
		parts = append(parts, strings.Join(run.Upgraded, " "))
	default:
		obs.Title = "Automatic updates found nothing to install"
	}
	if rebootRequired {
		if obs.Severity == alerts.SeverityInfo {
			obs.Severity = alerts.SeverityWarning
		}
		parts = append(parts, "Reboot required.")
	}
	obs.Message = truncateMessage(strings.Join(parts, "\n"))
	return obs, true
}
