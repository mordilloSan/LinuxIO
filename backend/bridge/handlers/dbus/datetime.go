package dbus

import (
	"fmt"
	"os"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"
)

const timedateBus = "org.freedesktop.timedate1"
const timedatePath = "/org/freedesktop/timedate1"
const timesyncdConf = "/etc/systemd/timesyncd.conf"

func readTimedateProperty(prop string) (string, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	var result string
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
		obj := conn.Object(timedateBus, timedatePath)
		var variant godbus.Variant
		if err = obj.Call("org.freedesktop.DBus.Properties.Get", 0, timedateBus, prop).Store(&variant); err != nil {
			return err
		}
		s, ok := variant.Value().(string)
		if !ok {
			return fmt.Errorf("%s property not a string (got %T)", prop, variant.Value())
		}
		result = s
		return nil
	})
	return result, err
}

func readTimedateBoolProperty(prop string) (bool, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	var result bool
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
		obj := conn.Object(timedateBus, timedatePath)
		var variant godbus.Variant
		if err = obj.Call("org.freedesktop.DBus.Properties.Get", 0, timedateBus, prop).Store(&variant); err != nil {
			return err
		}
		b, ok := variant.Value().(bool)
		if !ok {
			return fmt.Errorf("%s property not a bool (got %T)", prop, variant.Value())
		}
		result = b
		return nil
	})
	return result, err
}

func GetNTPStatus() (bool, error) {
	return readTimedateBoolProperty("NTP")
}

func GetTimezone() (string, error) {
	return readTimedateProperty("Timezone")
}

func SetTimezone(tz string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
		obj := conn.Object(timedateBus, timedatePath)
		return obj.Call(timedateBus+".SetTimezone", 0, tz, false).Err
	})
}

func SetNTP(enabled bool) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
		obj := conn.Object(timedateBus, timedatePath)
		return obj.Call(timedateBus+".SetNTP", 0, enabled, false).Err
	})
}

func SetServerTime(isoTime string) error {
	t, err := time.Parse(time.RFC3339, isoTime)
	if err != nil {
		return fmt.Errorf("invalid time format (expected RFC3339): %w", err)
	}
	usec := t.UnixMicro()
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
		obj := conn.Object(timedateBus, timedatePath)
		return obj.Call(timedateBus+".SetTime", 0, usec, false, false).Err
	})
}

// GetNTPServers reads custom NTP servers from /etc/systemd/timesyncd.conf.
// Returns an empty slice if none are configured (meaning defaults are used).
func GetNTPServers() ([]string, error) {
	data, err := os.ReadFile(timesyncdConf)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
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
			value := strings.TrimPrefix(trimmed, "NTP=")
			if value = strings.TrimSpace(value); value == "" {
				return []string{}, nil
			}
			return strings.Fields(value), nil
		}
	}
	return []string{}, nil
}

// SetNTPServers writes custom NTP servers to /etc/systemd/timesyncd.conf
// and restarts systemd-timesyncd to apply the change.
// Pass an empty slice to revert to default NTP servers.
func SetNTPServers(servers []string) error {
	data, err := os.ReadFile(timesyncdConf)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	ntpLine := "NTP=" + strings.Join(servers, " ")
	lines := setINISectionKey(strings.Split(string(data), "\n"), "[Time]", "NTP=", ntpLine)

	if err := os.WriteFile(timesyncdConf, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return err
	}
	return RestartService("systemd-timesyncd.service")
}

// setINISectionKey replaces or inserts a key line inside the given INI section.
func setINISectionKey(lines []string, section, keyPrefix, newLine string) []string {
	inSection := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == section {
			inSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inSection = false
			continue
		}
		if inSection && (strings.HasPrefix(trimmed, keyPrefix) || strings.HasPrefix(trimmed, "#"+keyPrefix)) {
			lines[i] = newLine
			return lines
		}
	}

	// Key not found — insert into (or create) the section.
	for i, line := range lines {
		if strings.TrimSpace(line) == section {
			rest := make([]string, len(lines[i+1:]))
			copy(rest, lines[i+1:])
			return append(lines[:i+1], append([]string{newLine}, rest...)...)
		}
	}

	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	return append(lines, section, newLine)
}
