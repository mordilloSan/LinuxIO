package watchtower

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	ModeUpdate          = "update"
	ModeCheckOnly       = "check_only"
	DefaultScheduleTime = "04:00"
)

var dailyTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

type ScheduleConfig struct {
	Mode           string
	Time           string
	Cleanup        bool
	ContainerNames []string
}

func DefaultScheduleConfig() ScheduleConfig {
	return ScheduleConfig{
		Mode:           ModeUpdate,
		Time:           DefaultScheduleTime,
		Cleanup:        false,
		ContainerNames: nil,
	}
}

func NormalizeScheduleConfig(cfg ScheduleConfig) (ScheduleConfig, error) {
	mode, err := NormalizeMode(cfg.Mode)
	if err != nil {
		return ScheduleConfig{}, err
	}
	timeOfDay, err := NormalizeTime(cfg.Time)
	if err != nil {
		return ScheduleConfig{}, err
	}
	return ScheduleConfig{
		Mode:           mode,
		Time:           timeOfDay,
		Cleanup:        cfg.Cleanup,
		ContainerNames: NormalizeContainerNames(cfg.ContainerNames),
	}, nil
}

func NormalizeMode(mode string) (string, error) {
	switch strings.TrimSpace(mode) {
	case "", ModeUpdate:
		return ModeUpdate, nil
	case ModeCheckOnly:
		return ModeCheckOnly, nil
	default:
		return "", fmt.Errorf("invalid watchtower mode %q", mode)
	}
}

func NormalizeTime(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = DefaultScheduleTime
	}
	if !dailyTimePattern.MatchString(value) {
		return "", fmt.Errorf("invalid watchtower time %q", raw)
	}
	return value, nil
}

func NormalizeContainerNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func RenderEnv(cfg ScheduleConfig) ([]byte, error) {
	normalized, err := NormalizeScheduleConfig(cfg)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("# Managed by LinuxIO. Edit through LinuxIO.\n")
	buf.WriteString("# LINUXIO_WATCHTOWER_CONTAINERS is a whitespace-separated list of quoted container names.\n")
	buf.WriteString("WATCHTOWER_NO_STARTUP_MESSAGE=true\n")
	if normalized.Mode == ModeCheckOnly {
		buf.WriteString("WATCHTOWER_MONITOR_ONLY=true\n")
	}
	if normalized.Cleanup {
		buf.WriteString("WATCHTOWER_CLEANUP=true\n")
	}
	buf.WriteString("LINUXIO_WATCHTOWER_CONTAINERS=")
	buf.WriteString(RenderContainerEnvValue(normalized.ContainerNames))
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func ParseEnv(data []byte) ScheduleConfig {
	cfg := DefaultScheduleConfig()
	assignments := parseEnvAssignments(string(data))
	if isTruthy(assignments["WATCHTOWER_MONITOR_ONLY"]) {
		cfg.Mode = ModeCheckOnly
	}
	cfg.Cleanup = isTruthy(assignments["WATCHTOWER_CLEANUP"])
	cfg.ContainerNames = ParseContainerEnvValue(assignments["LINUXIO_WATCHTOWER_CONTAINERS"])
	return cfg
}

func RenderTimer(timeOfDay string) ([]byte, error) {
	normalized, err := NormalizeTime(timeOfDay)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`[Unit]
Description=Run LinuxIO Watchtower container updates
Documentation=https://github.com/nicholas-fedor/watchtower

[Timer]
OnCalendar=*-*-* %s:00
Persistent=true
Unit=%s

[Install]
WantedBy=timers.target
`, normalized, UnitName)), nil
}

func ParseTimer(data []byte) string {
	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, ok := splitEnvAssignment(line)
		if !ok || key != "OnCalendar" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 2 {
			continue
		}
		timeValue := strings.TrimSuffix(fields[1], ":00")
		if normalized, err := NormalizeTime(timeValue); err == nil {
			return normalized
		}
	}
	return DefaultScheduleTime
}

func RenderContainerEnvValue(names []string) string {
	normalized := NormalizeContainerNames(names)
	if len(normalized) == 0 {
		return NoContainersID
	}
	tokens := make([]string, 0, len(normalized))
	for _, name := range normalized {
		tokens = append(tokens, EnvTokenForName(name))
	}
	return strings.Join(tokens, " ")
}

func ParseContainerEnvValue(value string) []string {
	var names []string
	for token := range strings.FieldsSeq(value) {
		if token == "" || token == NoContainersID {
			continue
		}
		names = append(names, NameFromEnvToken(token))
	}
	return NormalizeContainerNames(names)
}

func EnvTokenForName(name string) string {
	return strings.ReplaceAll(QuoteName(name), `\`, `\\`)
}

func NameFromEnvToken(token string) string {
	systemdValue := strings.ReplaceAll(token, `\\`, `\`)
	var b strings.Builder
	escaped := false
	for _, r := range systemdValue {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	return b.String()
}

func parseEnvAssignments(data string) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(data, "\n") {
		key, value, ok := splitEnvAssignment(line)
		if ok {
			out[key] = value
		}
	}
	return out
}

func splitEnvAssignment(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
}

func isTruthy(value string) bool {
	return slices.Contains([]string{"1", "true", "yes", "on"}, strings.ToLower(strings.TrimSpace(value)))
}
