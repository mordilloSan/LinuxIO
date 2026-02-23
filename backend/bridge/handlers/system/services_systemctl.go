package system

import (
	"context"
	"errors"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"
)

type ServiceInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	MainPID     int32  `json:"main_pid"`
	Failed      bool   `json:"failed"`
}

func FetchServices() ([]ServiceInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		services []ServiceInfo
		err      error
	}
	done := make(chan result, 1)
	go func() {
		services, err := fetchServicesViaDBus()
		done <- result{services: services, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, errors.New("systemd query timed out")
	case res := <-done:
		return res.services, res.err
	}
}

func fetchServicesViaDBus() ([]ServiceInfo, error) {
	conn, err := godbus.SystemBus()
	if err != nil {
		return nil, errors.New("failed to connect to systemd D-Bus; system may not be running systemd")
	}
	defer conn.Close()

	systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	var units [][]any
	if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&units); err != nil {
		return nil, err
	}

	services := make([]ServiceInfo, 0, len(units))
	for _, unit := range units {
		name, ok := getStringField(unit, 0)
		if !ok || !strings.HasSuffix(name, ".service") {
			continue
		}

		description, _ := getStringField(unit, 1)
		activeState, _ := getStringField(unit, 3)
		subState, _ := getStringField(unit, 4)

		info := ServiceInfo{
			Name:        name,
			Description: description,
			ActiveState: activeState,
			SubState:    subState,
			Failed:      activeState == "failed" || subState == "failed",
		}

		unitPath, ok := getObjectPathField(unit, 6)
		if ok && unitPath != "" && unitPath != "/" {
			unitObj := conn.Object("org.freedesktop.systemd1", unitPath)
			if prop, err := unitObj.GetProperty("org.freedesktop.systemd1.Service.MainPID"); err == nil {
				info.MainPID = toInt32(prop.Value())
			}
		}

		services = append(services, info)
	}

	return services, nil
}

func getStringField(fields []any, index int) (string, bool) {
	if index < 0 || index >= len(fields) {
		return "", false
	}

	s, ok := fields[index].(string)
	return s, ok
}

func getObjectPathField(fields []any, index int) (godbus.ObjectPath, bool) {
	if index < 0 || index >= len(fields) {
		return "", false
	}

	path, ok := fields[index].(godbus.ObjectPath)
	return path, ok
}

func toInt32(v any) int32 {
	switch n := v.(type) {
	case uint32:
		return int32(n)
	case uint64:
		return int32(n)
	case int32:
		return n
	case int64:
		return int32(n)
	case int:
		return int32(n)
	default:
		return 0
	}
}
