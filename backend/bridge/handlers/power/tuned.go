package power

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/systemd"
)

const (
	tunedBusName       = "com.redhat.tuned"
	tunedObjectPath    = godbus.ObjectPath("/Tuned")
	tunedControlIface  = "com.redhat.tuned.control"
	ppdBusName         = "org.freedesktop.UPower.PowerProfiles"
	tunedUnitName      = "tuned.service"
	tunedPackageName   = "tuned"
	defaultInstallHint = "Install the tuned package with your distribution package manager"
)

var (
	ErrUnavailable = errors.New("TuneD D-Bus service is unavailable")
	profileNameRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:+-]*$`)
)

type tunedProfileRecord struct {
	Name        string
	Description string
}

func Available() (bool, error) {
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return false, fmt.Errorf("connect to system bus: %w", err)
	}
	defer conn.Close()

	state, err := readNameState(conn, tunedBusName)
	if err != nil {
		return false, err
	}
	availability := readTunedUnitAvailability()
	return state.active || state.activatable || availability.available, nil
}

func GetStatus() (PowerStatus, error) {
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return PowerStatus{}, fmt.Errorf("connect to system bus: %w", err)
	}
	defer conn.Close()

	return getStatus(conn)
}

func StartTuned() (PowerStatus, error) {
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return PowerStatus{}, fmt.Errorf("connect to system bus: %w", err)
	}
	defer conn.Close()

	if err := ensureTunedRunning(conn); err != nil {
		return getStatusWithError(conn, err)
	}
	return getStatus(conn)
}

func SetProfile(profile string) (PowerStatus, error) {
	profile = strings.TrimSpace(profile)
	if !profileNameRE.MatchString(profile) {
		return PowerStatus{}, fmt.Errorf("invalid TuneD profile name")
	}

	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return PowerStatus{}, fmt.Errorf("connect to system bus: %w", err)
	}
	defer conn.Close()

	status, err := getStatus(conn)
	if err != nil {
		return status, err
	}
	if !status.TunedAvailable {
		return status, ErrUnavailable
	}

	if !status.TunedActive {
		if startErr := ensureTunedRunning(conn); startErr != nil {
			return status, startErr
		}
		status, _ = getStatus(conn)
	}

	if len(status.Profiles) > 0 && !slices.ContainsFunc(status.Profiles, func(p TunedProfile) bool { return p.Name == profile }) {
		return status, fmt.Errorf("TuneD profile %q is not available", profile)
	}

	ok, message, err := switchProfile(conn, profile)
	if err != nil {
		return status, err
	}
	if !ok {
		if message == "" {
			message = "TuneD refused the profile change"
		}
		return status, errors.New(message)
	}

	return getStatus(conn)
}

func DisableTuned() (PowerStatus, error) {
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return PowerStatus{}, fmt.Errorf("connect to system bus: %w", err)
	}
	defer conn.Close()

	status, err := getStatus(conn)
	if err != nil {
		return status, err
	}
	if !status.TunedAvailable {
		return status, ErrUnavailable
	}

	ok, err := disableTuned(conn)
	if err != nil {
		return status, err
	}
	if !ok {
		return status, fmt.Errorf("TuneD did not disable tunings")
	}
	return getStatus(conn)
}

func getStatus(conn *godbus.Conn) (PowerStatus, error) {
	status := baseStatus()

	tunedState, err := readNameState(conn, tunedBusName)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}

	ppdState, ppdErr := readNameState(conn, ppdBusName)
	if ppdErr == nil {
		status.PowerProfilesDaemonActive = ppdState.active
	}

	status.TunedActive = tunedState.active
	status.TunedActivatable = tunedState.activatable
	unitAvailability := readTunedUnitAvailability()
	status.TunedUnitAvailable = unitAvailability.available
	status.TunedUnitFileState = unitAvailability.state
	status.TunedStartable = tunedState.active || tunedState.activatable || unitAvailability.startable
	status.TunedAvailable = tunedState.active || tunedState.activatable || unitAvailability.available

	switch {
	case !status.TunedAvailable:
		status.Notes = append(status.Notes, "TuneD is not available on the system bus or as a systemd service.")
		return status, nil
	case !status.TunedActive:
		status.Notes = append(status.Notes, "TuneD is installed but not running.")
		return status, nil
	}

	if status.PowerProfilesDaemonActive {
		status.Notes = append(status.Notes, "power-profiles-daemon is active and may conflict with TuneD.")
	}

	if running, runErr := isTunedRunning(conn); runErr == nil {
		status.TunedActive = running
		if !running {
			status.Notes = append(status.Notes, "TuneD tunings are currently disabled.")
		}
	}

	profiles, err := readProfiles(conn)
	if err != nil {
		status.Error = err.Error()
	} else {
		status.Profiles = profiles
	}

	if active, err := activeProfile(conn); err == nil {
		status.ActiveProfile = active
	} else if status.Error == "" {
		status.Error = err.Error()
	}

	if recommended, err := recommendedProfile(conn); err == nil {
		status.RecommendedProfile = recommended
	}

	markProfiles(status.Profiles, status.ActiveProfile, status.RecommendedProfile)
	return status, nil
}

func getStatusWithError(conn *godbus.Conn, cause error) (PowerStatus, error) {
	status, _ := getStatus(conn)
	return status, cause
}

func baseStatus() PowerStatus {
	return PowerStatus{
		Backend:        "tuned",
		PackageName:    tunedPackageName,
		InstallCommand: installCommandHint(),
		Profiles:       make([]TunedProfile, 0),
	}
}

type nameState struct {
	active      bool
	activatable bool
}

type unitAvailability struct {
	available bool
	startable bool
	state     string
}

func readNameState(conn *godbus.Conn, name string) (nameState, error) {
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return nameState{}, fmt.Errorf("list D-Bus names: %w", err)
	}

	var activatable []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return nameState{}, fmt.Errorf("list activatable D-Bus names: %w", err)
	}

	return nameState{
		active:      slices.Contains(names, name),
		activatable: slices.Contains(activatable, name),
	}, nil
}

func activateTuned(conn *godbus.Conn) error {
	var result uint32
	if err := conn.BusObject().Call("org.freedesktop.DBus.StartServiceByName", 0, tunedBusName, uint32(0)).Store(&result); err != nil {
		return fmt.Errorf("start TuneD D-Bus service: %w", err)
	}
	return nil
}

func readTunedUnitAvailability() unitAvailability {
	state, err := systemdapi.GetUnitFileState(tunedUnitName)
	if err != nil {
		return unitAvailability{}
	}

	return unitAvailability{
		available: isKnownUnitFileState(state),
		startable: isStartableUnitFileState(state),
		state:     state,
	}
}

func isKnownUnitFileState(state string) bool {
	return state != "" && state != "bad" && state != "not-found"
}

func isStartableUnitFileState(state string) bool {
	switch state {
	case "masked", "masked-runtime", "bad", "not-found", "":
		return false
	default:
		return true
	}
}

func ensureTunedRunning(conn *godbus.Conn) error {
	state, err := readNameState(conn, tunedBusName)
	if err != nil {
		return err
	}

	if !state.active {
		if startErr := startTunedDaemon(conn, state); startErr != nil {
			return startErr
		}
		if waitErr := waitForTunedBus(conn); waitErr != nil {
			return waitErr
		}
	}

	if running, runErr := isTunedRunning(conn); runErr == nil && running {
		return nil
	}

	ok, err := startTunedTunings(conn)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	profile, err := ensureTunedProfile(conn)
	if err != nil {
		return err
	}

	ok, err = startTunedTunings(conn)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("TuneD did not start tunings after applying profile %q", profile)
	}
	return nil
}

func startTunedDaemon(conn *godbus.Conn, state nameState) error {
	if state.activatable {
		return activateTuned(conn)
	}

	availability := readTunedUnitAvailability()
	if !availability.startable {
		return fmt.Errorf("TuneD is installed but %s is not startable", tunedUnitName)
	}
	if err := systemdapi.StartUnit(tunedUnitName); err != nil {
		return fmt.Errorf("start %s through systemd D-Bus: %w", tunedUnitName, err)
	}
	return nil
}

func waitForTunedBus(conn *godbus.Conn) error {
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, err := readNameState(conn, tunedBusName)
		if err != nil {
			return err
		}
		if state.active {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("TuneD D-Bus service did not appear after starting %s", tunedUnitName)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func tunedObject(conn *godbus.Conn) godbus.BusObject {
	return conn.Object(tunedBusName, tunedObjectPath)
}

func isTunedRunning(conn *godbus.Conn) (bool, error) {
	var running bool
	err := tunedObject(conn).Call(tunedControlIface+".is_running", 0).Store(&running)
	return running, err
}

func activeProfile(conn *godbus.Conn) (string, error) {
	var profile string
	err := tunedObject(conn).Call(tunedControlIface+".active_profile", 0).Store(&profile)
	return profile, err
}

func recommendedProfile(conn *godbus.Conn) (string, error) {
	var profile string
	err := tunedObject(conn).Call(tunedControlIface+".recommend_profile", 0).Store(&profile)
	return profile, err
}

func readProfiles(conn *godbus.Conn) ([]TunedProfile, error) {
	var records []tunedProfileRecord
	if err := tunedObject(conn).Call(tunedControlIface+".profiles2", 0).Store(&records); err == nil {
		return profilesFromRecords(records), nil
	}

	var names []string
	if err := tunedObject(conn).Call(tunedControlIface+".profiles", 0).Store(&names); err != nil {
		return nil, err
	}
	profiles := make([]TunedProfile, 0, len(names))
	for _, name := range names {
		profiles = append(profiles, TunedProfile{Name: name})
	}
	return profiles, nil
}

func profilesFromRecords(records []tunedProfileRecord) []TunedProfile {
	profiles := make([]TunedProfile, 0, len(records))
	for _, record := range records {
		profiles = append(profiles, TunedProfile{
			Name:        record.Name,
			Description: record.Description,
		})
	}
	return profiles
}

func switchProfile(conn *godbus.Conn, profile string) (bool, string, error) {
	call := tunedObject(conn).Call(tunedControlIface+".switch_profile", 0, profile)
	if call.Err != nil {
		return false, "", call.Err
	}
	return boolStringResult(call.Body)
}

func autoProfile(conn *godbus.Conn) (bool, string, error) {
	call := tunedObject(conn).Call(tunedControlIface+".auto_profile", 0)
	if call.Err != nil {
		return false, "", call.Err
	}
	return boolStringResult(call.Body)
}

func disableTuned(conn *godbus.Conn) (bool, error) {
	var ok bool
	err := tunedObject(conn).Call(tunedControlIface+".disable", 0).Store(&ok)
	return ok, err
}

func startTunedTunings(conn *godbus.Conn) (bool, error) {
	var ok bool
	err := tunedObject(conn).Call(tunedControlIface+".start", 0).Store(&ok)
	return ok, err
}

func ensureTunedProfile(conn *godbus.Conn) (string, error) {
	if active, err := activeProfile(conn); err == nil && active != "" {
		return active, nil
	}

	recommended, err := recommendedProfile(conn)
	if err != nil {
		return "", err
	}
	if recommended == "" {
		return "", fmt.Errorf("TuneD has no active or recommended profile")
	}

	ok, message, err := autoProfile(conn)
	if err != nil {
		return "", err
	}
	if !ok {
		if message == "" {
			message = "TuneD refused automatic profile selection"
		}
		return "", errors.New(message)
	}
	return recommended, nil
}

func boolStringResult(body []any) (bool, string, error) {
	if len(body) == 2 {
		ok, _ := body[0].(bool)
		msg, _ := body[1].(string)
		return ok, msg, nil
	}
	if len(body) == 1 {
		if values, ok := body[0].([]any); ok && len(values) == 2 {
			accepted, _ := values[0].(bool)
			msg, _ := values[1].(string)
			return accepted, msg, nil
		}
		value := reflect.ValueOf(body[0])
		if value.Kind() == reflect.Struct && value.NumField() == 2 {
			okField := value.Field(0)
			msgField := value.Field(1)
			if okField.Kind() == reflect.Bool && msgField.Kind() == reflect.String {
				return okField.Bool(), msgField.String(), nil
			}
		}
	}
	return false, "", fmt.Errorf("unexpected TuneD result signature")
}

func markProfiles(profiles []TunedProfile, active, recommended string) {
	for i := range profiles {
		profiles[i].Active = profiles[i].Name == active
		profiles[i].Recommended = profiles[i].Name == recommended
	}
}

func installCommandHint() string {
	id, idLike := readOSReleaseIDs()
	switch {
	case distroMatches(id, idLike, "debian", "ubuntu", "linuxmint", "pop"):
		return "sudo apt install tuned"
	case distroMatches(id, idLike, "rhel", "fedora", "centos", "rocky", "almalinux"):
		return "sudo dnf install tuned"
	default:
		return defaultInstallHint
	}
}

func distroMatches(id string, idLike []string, values ...string) bool {
	for _, value := range values {
		if id == value || slices.Contains(idLike, value) {
			return true
		}
	}
	return false
}

func readOSReleaseIDs() (string, []string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", nil
	}
	values := parseOSRelease(data)
	id := strings.ToLower(values["ID"])
	idLike := strings.Fields(strings.ToLower(values["ID_LIKE"]))
	return id, idLike
}

func parseOSRelease(data []byte) map[string]string {
	values := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values
}
