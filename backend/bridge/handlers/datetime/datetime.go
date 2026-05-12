package datetime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsutil"
)

const (
	timesyncdMainConf    = "/etc/systemd/timesyncd.conf"
	timesyncdManagedConf = "/etc/systemd/timesyncd.conf.d/linuxio.conf"

	chronyInlineStartMarker = "# LinuxIO managed NTP sources start"
	chronyInlineEndMarker   = "# LinuxIO managed NTP sources end"
)

var (
	chronyMainConfCandidates = []string{
		"/etc/chrony.conf",
		"/etc/chrony/chrony.conf",
	}
	chronyServiceCandidates = []string{
		"chronyd.service",
		"chrony.service",
	}
)

type ntpServerBackend interface {
	GetServers(context.Context) ([]string, error)
	SetServers(context.Context, []string) error
}

type backendCandidate struct {
	score   int
	backend ntpServerBackend
}

type timesyncdBackend struct{}

type chronyBackend struct {
	mainPath      string
	managedPath   string
	inlineManaged bool
}

var timedateIface = dbusclient.Timedate.Interface(dbusclient.TimedateIface)

func readTimedateProperty(ctx context.Context, prop string) (string, error) {
	var result string
	err := withTimedateSession(ctx, func(session dbusclient.SystemSession) error {
		var err error
		result, err = dbusclient.GetProperty[string](session.Context(), session.Object(), dbusclient.TimedateIface, prop)
		return err
	})
	return result, err
}

func readTimedateBoolProperty(ctx context.Context, prop string) (bool, error) {
	var result bool
	err := withTimedateSession(ctx, func(session dbusclient.SystemSession) error {
		var err error
		result, err = dbusclient.GetProperty[bool](session.Context(), session.Object(), dbusclient.TimedateIface, prop)
		return err
	})
	return result, err
}

func GetNTPStatus(ctx context.Context) (bool, error) {
	return readTimedateBoolProperty(ctx, "NTP")
}

func GetTimezone(ctx context.Context) (string, error) {
	return readTimedateProperty(ctx, "Timezone")
}

func SetTimezone(ctx context.Context, tz string) error {
	return callTimedate(ctx, "SetTimezone", tz, false)
}

func SetNTP(ctx context.Context, enabled bool) error {
	return callTimedate(ctx, "SetNTP", enabled, false)
}

func SetServerTime(ctx context.Context, isoTime string) error {
	t, err := time.Parse(time.RFC3339, isoTime)
	if err != nil {
		return fmt.Errorf("invalid time format (expected RFC3339): %w", err)
	}
	usec := t.UnixMicro()
	return callTimedate(ctx, "SetTime", usec, false, false)
}

func callTimedate(ctx context.Context, member string, args ...any) error {
	return withTimedateSession(ctx, func(session dbusclient.SystemSession) error {
		return session.Call(timedateIface.Method(member), dbusclient.CallPolicy{}, args...)
	})
}

func withTimedateSession(ctx context.Context, fn func(dbusclient.SystemSession) error) error {
	return dbusclient.Timedate.UseSession(ctx, fn)
}

func GetNTPServers(ctx context.Context) ([]string, error) {
	backend, err := selectNTPServerBackend(ctx)
	if err != nil {
		return []string{}, nil
	}
	return backend.GetServers(ctx)
}

func SetNTPServers(ctx context.Context, servers []string) error {
	backend, err := selectNTPServerBackend(ctx)
	if err != nil {
		return err
	}
	return backend.SetServers(ctx, servers)
}

func selectNTPServerBackend(ctx context.Context) (ntpServerBackend, error) {
	candidates := []backendCandidate{
		detectChronyBackend(ctx),
		detectTimesyncdBackend(ctx),
	}
	best := backendCandidate{}
	for _, candidate := range candidates {
		if candidate.score > best.score {
			best = candidate
		}
	}
	if best.backend == nil {
		return nil, fmt.Errorf("no supported NTP server backend found")
	}
	return best.backend, nil
}

func detectTimesyncdBackend(ctx context.Context) backendCandidate {
	score := serviceScore(ctx, []string{"systemd-timesyncd.service"})
	if fileExists(timesyncdManagedConf) {
		score = max(score, 150)
	}
	if fileExists(timesyncdMainConf) {
		score = max(score, 100)
	}
	if score == 0 {
		return backendCandidate{}
	}
	return backendCandidate{
		score:   score,
		backend: timesyncdBackend{},
	}
}

func detectChronyBackend(ctx context.Context) backendCandidate {
	mainPath, mainExists := firstExistingPath(chronyMainConfCandidates)
	score := serviceScore(ctx, chronyServiceCandidates)
	if mainExists {
		score = max(score, 100)
	}
	if score == 0 {
		return backendCandidate{}
	}
	if !mainExists {
		mainPath = chronyMainConfCandidates[0]
	}
	managedPath, inlineManaged := chronyManagedTarget(mainPath)
	if fileExists(managedPath) {
		score = max(score, 160)
	}
	return backendCandidate{
		score: score,
		backend: chronyBackend{
			mainPath:      mainPath,
			managedPath:   managedPath,
			inlineManaged: inlineManaged,
		},
	}
}

func (timesyncdBackend) GetServers(context.Context) ([]string, error) {
	for _, path := range []string{timesyncdManagedConf, timesyncdMainConf} {
		servers, found, err := parseTimesyncdServers(path)
		if err != nil {
			return nil, err
		}
		if found {
			return servers, nil
		}
	}
	return []string{}, nil
}

func (timesyncdBackend) SetServers(ctx context.Context, servers []string) error {
	if len(servers) == 0 {
		if err := os.Remove(timesyncdManagedConf); err != nil && !os.IsNotExist(err) {
			return err
		}
		return restartFirstService(ctx, []string{"systemd-timesyncd.service"})
	}

	content := "[Time]\nNTP=" + strings.Join(servers, " ") + "\n"
	if err := fsutil.WriteFileAtomic(timesyncdManagedConf, []byte(content), 0o644); err != nil {
		return err
	}
	return restartFirstService(ctx, []string{"systemd-timesyncd.service"})
}

func (b chronyBackend) GetServers(context.Context) ([]string, error) {
	if b.inlineManaged {
		if servers, found, err := parseChronyInlineManagedServers(b.mainPath); err != nil {
			return nil, err
		} else if found {
			return servers, nil
		}
	}
	if b.managedPath != "" && !b.inlineManaged {
		if servers, found, err := parseChronyServersFromFile(b.managedPath); err != nil {
			return nil, err
		} else if found {
			return servers, nil
		}
	}
	if b.mainPath == "" {
		return []string{}, nil
	}
	servers, _, err := parseChronyServersFromFile(b.mainPath)
	return servers, err
}

func (b chronyBackend) SetServers(ctx context.Context, servers []string) error {
	if b.inlineManaged {
		if err := b.writeInlineManagedServers(servers); err != nil {
			return err
		}
		return restartFirstService(ctx, chronyServiceCandidates)
	}
	if len(servers) == 0 {
		if err := os.Remove(b.managedPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return restartFirstService(ctx, chronyServiceCandidates)
	}
	content := renderChronyManagedServers(servers)
	if err := fsutil.WriteFileAtomic(b.managedPath, []byte(content), 0o644); err != nil {
		return err
	}
	return restartFirstService(ctx, chronyServiceCandidates)
}

func (b chronyBackend) writeInlineManagedServers(servers []string) error {
	data, err := os.ReadFile(b.mainPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(data), "\n")
	block := buildChronyManagedBlock(servers)
	updated := replaceManagedBlock(lines, chronyInlineStartMarker, chronyInlineEndMarker, block)
	return fsutil.WriteFileAtomic(b.mainPath, []byte(strings.Join(updated, "\n")), 0o644)
}

func parseTimesyncdServers(path string) ([]string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	inTimeSection := false
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[Time]" {
			inTimeSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inTimeSection = false
			continue
		}
		if inTimeSection && strings.HasPrefix(trimmed, "NTP=") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "NTP="))
			if value == "" {
				return []string{}, true, nil
			}
			return strings.Fields(value), true, nil
		}
	}
	return nil, false, nil
}

func parseChronyServersFromFile(path string) ([]string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	servers := parseChronyServers(string(data))
	return servers, len(servers) > 0, nil
}

func parseChronyInlineManagedServers(path string) ([]string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	lines := strings.Split(string(data), "\n")
	block := extractManagedBlock(lines, chronyInlineStartMarker, chronyInlineEndMarker)
	if len(block) == 0 {
		return nil, false, nil
	}
	return parseChronyServers(strings.Join(block, "\n")), true, nil
}

func parseChronyServers(data string) []string {
	var servers []string
	for line := range strings.SplitSeq(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "server", "pool", "peer":
			servers = append(servers, fields[1])
		}
	}
	return servers
}

func renderChronyManagedServers(servers []string) string {
	return strings.Join(buildChronyManagedBlock(servers), "\n") + "\n"
}

func buildChronyManagedBlock(servers []string) []string {
	if len(servers) == 0 {
		return nil
	}
	lines := []string{
		chronyInlineStartMarker,
		"# Managed by LinuxIO",
	}
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		lines = append(lines, "server "+server+" iburst")
	}
	lines = append(lines, chronyInlineEndMarker)
	return lines
}

func replaceManagedBlock(lines []string, startMarker, endMarker string, newBlock []string) []string {
	start, end := findManagedBlock(lines, startMarker, endMarker)
	switch {
	case start >= 0 && end >= start:
		replaced := append([]string{}, lines[:start]...)
		if len(newBlock) > 0 {
			replaced = append(replaced, newBlock...)
		}
		replaced = append(replaced, lines[end+1:]...)
		return trimTrailingBlankLines(replaced)
	case len(newBlock) == 0:
		return trimTrailingBlankLines(lines)
	default:
		result := trimTrailingBlankLines(lines)
		if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
			result = append(result, "")
		}
		return append(result, newBlock...)
	}
}

func extractManagedBlock(lines []string, startMarker, endMarker string) []string {
	start, end := findManagedBlock(lines, startMarker, endMarker)
	if start < 0 || end < start {
		return nil
	}
	return append([]string{}, lines[start+1:end]...)
}

func findManagedBlock(lines []string, startMarker, endMarker string) (int, int) {
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == startMarker {
			start = i
			continue
		}
		if start >= 0 && trimmed == endMarker {
			return start, i
		}
	}
	return -1, -1
}

func trimTrailingBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func chronyManagedTarget(mainPath string) (string, bool) {
	if mainPath == "" {
		return "", false
	}
	data, err := os.ReadFile(mainPath)
	if err == nil {
		if path, ok := chronyIncludedManagedPath(string(data)); ok {
			return path, false
		}
	}
	return mainPath, true
}

func chronyIncludedManagedPath(data string) (string, bool) {
	var sourceDirs []string
	var confDirs []string
	for line := range strings.SplitSeq(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "sourcedir":
			sourceDirs = append(sourceDirs, fields[1])
		case "confdir":
			confDirs = append(confDirs, fields[1])
		}
	}

	for _, dir := range sourceDirs {
		if strings.HasPrefix(dir, "/etc/") {
			return filepath.Join(dir, "linuxio.sources"), true
		}
	}
	for _, dir := range confDirs {
		if strings.HasPrefix(dir, "/etc/") {
			return filepath.Join(dir, "linuxio.conf"), true
		}
	}
	return "", false
}

func serviceScore(ctx context.Context, candidates []string) int {
	score := 0
	for _, name := range candidates {
		activeState, activeErr := systemd.GetActiveState(ctx, name)
		if activeErr == nil {
			switch activeState {
			case "active":
				score = max(score, 300)
			case "activating", "reloading":
				score = max(score, 250)
			}
		}
		unitFileState, stateErr := systemd.GetUnitFileState(ctx, name)
		if stateErr == nil && unitFileState != "masked" && unitFileState != "disabled" {
			score = max(score, 200)
		}
	}
	return score
}

func restartFirstService(ctx context.Context, candidates []string) error {
	for _, name := range candidates {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, err := systemd.GetUnitFileState(ctx, name); err == nil {
			return systemd.RestartUnit(ctx, name)
		}
		if state, err := systemd.GetActiveState(ctx, name); err == nil && state != "inactive" {
			return systemd.RestartUnit(ctx, name)
		}
	}
	return fmt.Errorf("no available service to restart: %s", strings.Join(candidates, ", "))
}

func firstExistingPath(candidates []string) (string, bool) {
	for _, path := range candidates {
		if fileExists(path) {
			return path, true
		}
	}
	return "", false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
