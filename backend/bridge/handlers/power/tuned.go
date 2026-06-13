package power

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	tunedBusName       = dbusclient.TunedBusName
	tunedControlIface  = dbusclient.TunedControlIface
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

type tunedBoolStringResult struct {
	OK      bool
	Message string
}

func Available(ctx context.Context) (bool, error) {
	state, err := dbusclient.Tuned.BusNameState(ctx)
	if err != nil {
		return false, err
	}
	// TuneD is not always D-Bus activatable when installed; the systemd unit
	// fallback lets the bridge report and start tuned.service directly.
	availability := readTunedUnitAvailability(ctx)
	return state.Available() || availability.available, nil
}

func GetStatus(ctx context.Context) (apischema.PowerStatus, error) {
	var status apischema.PowerStatus
	err := withTunedSession(ctx, func(session dbusclient.SystemSession) error {
		var err error
		status, err = getStatus(session)
		return err
	})
	return status, err
}

func StartTuned(ctx context.Context) (apischema.PowerStatus, error) {
	var status apischema.PowerStatus
	err := withTunedSession(ctx, func(session dbusclient.SystemSession) error {
		if err := ensureTunedRunning(session); err != nil {
			status, _ = getStatus(session)
			return err
		}
		var err error
		status, err = getStatus(session)
		return err
	})
	return status, err
}

func SetProfile(ctx context.Context, profile string) (apischema.PowerStatus, error) {
	profile = strings.TrimSpace(profile)
	if !profileNameRE.MatchString(profile) {
		return apischema.PowerStatus{}, fmt.Errorf("invalid TuneD profile name")
	}

	var status apischema.PowerStatus
	err := withTunedSession(ctx, func(session dbusclient.SystemSession) error {
		var err error
		status, err = getStatus(session)
		if err != nil {
			return err
		}
		if !status.TunedAvailable {
			return ErrUnavailable
		}

		if !status.TunedActive {
			if startErr := ensureTunedRunning(session); startErr != nil {
				return startErr
			}
			status, _ = getStatus(session)
		}

		if len(status.Profiles) > 0 && !slices.ContainsFunc(status.Profiles, func(p apischema.TunedProfile) bool { return p.Name == profile }) {
			return fmt.Errorf("TuneD profile %q is not available", profile)
		}

		ok, message, err := switchProfile(session, profile)
		if err != nil {
			return err
		}
		if !ok {
			if message == "" {
				message = "TuneD refused the profile change"
			}
			return errors.New(message)
		}

		status, err = getStatus(session)
		return err
	})
	return status, err
}

func DisableTuned(ctx context.Context) (apischema.PowerStatus, error) {
	var status apischema.PowerStatus
	err := withTunedSession(ctx, func(session dbusclient.SystemSession) error {
		var err error
		status, err = getStatus(session)
		if err != nil {
			return err
		}
		if !status.TunedAvailable {
			return ErrUnavailable
		}

		ok, err := disableTuned(session)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("TuneD did not disable tunings")
		}
		status, err = getStatus(session)
		return err
	})
	return status, err
}

func withTunedSession(ctx context.Context, fn func(dbusclient.SystemSession) error) error {
	return dbusclient.Tuned.UseSession(ctx, fn)
}

func getStatus(session dbusclient.SystemSession) (apischema.PowerStatus, error) {
	status := baseStatus()

	tunedState, err := dbusclient.Tuned.BusNameState(session.Context())
	if err != nil {
		status.Error = utils.OptionalString(err.Error())
		return status, nil
	}

	ppdState, ppdErr := dbusclient.PowerProfiles.BusNameState(session.Context())
	if ppdErr == nil {
		status.PowerProfilesDaemonActive = ppdState.Active
	}

	status.TunedActive = tunedState.Active
	status.TunedActivatable = tunedState.Activatable
	unitAvailability := readTunedUnitAvailability(session.Context())
	status.TunedUnitAvailable = unitAvailability.available
	status.TunedUnitFileState = unitAvailability.state
	status.TunedStartable = tunedState.Available() || unitAvailability.startable
	status.TunedAvailable = tunedState.Available() || unitAvailability.available

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

	if running, runErr := isTunedRunning(session); runErr == nil {
		status.TunedActive = running
		if !running {
			status.Notes = append(status.Notes, "TuneD tunings are currently disabled.")
		}
	}

	profiles, err := readProfiles(session)
	if err != nil {
		status.Error = utils.OptionalString(err.Error())
	} else {
		status.Profiles = profiles
	}

	if active, err := activeProfile(session); err == nil {
		status.ActiveProfile = active
	} else if status.Error == nil {
		status.Error = utils.OptionalString(err.Error())
	}

	if recommended, err := recommendedProfile(session); err == nil {
		status.RecommendedProfile = recommended
	}

	markProfiles(status.Profiles, status.ActiveProfile, status.RecommendedProfile)
	return status, nil
}

func baseStatus() apischema.PowerStatus {
	return apischema.PowerStatus{
		Backend:        "tuned",
		PackageName:    tunedPackageName,
		InstallCommand: installCommandHint(),
		Profiles:       make([]apischema.TunedProfile, 0),
	}
}

type unitAvailability struct {
	available bool
	startable bool
	state     string
}

func activateTuned(session dbusclient.SystemSession) error {
	if err := dbusclient.DBus.Interface(dbusclient.DBusIface).Call(
		session.Context(),
		"StartServiceByName",
		dbusclient.CallPolicy{},
		tunedBusName,
		uint32(0),
	); err != nil {
		return fmt.Errorf("start TuneD D-Bus service: %w", err)
	}
	return nil
}

func readTunedUnitAvailability(ctx context.Context) unitAvailability {
	state, err := systemdapi.GetUnitFileState(ctx, tunedUnitName)
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

func ensureTunedRunning(session dbusclient.SystemSession) error {
	state, err := dbusclient.Tuned.BusNameState(session.Context())
	if err != nil {
		return err
	}

	if !state.Active {
		if startErr := startTunedDaemon(session, state); startErr != nil {
			return startErr
		}
		if waitErr := waitForTunedBus(session); waitErr != nil {
			return waitErr
		}
	}

	if running, runErr := isTunedRunning(session); runErr == nil && running {
		return nil
	}

	ok, err := startTunedTunings(session)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	profile, err := ensureTunedProfile(session)
	if err != nil {
		return err
	}

	ok, err = startTunedTunings(session)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("TuneD did not start tunings after applying profile %q", profile)
	}
	return nil
}

func startTunedDaemon(session dbusclient.SystemSession, state dbusclient.BusNameState) error {
	if state.Activatable {
		return activateTuned(session)
	}

	availability := readTunedUnitAvailability(session.Context())
	if !availability.startable {
		return fmt.Errorf("TuneD is installed but %s is not startable", tunedUnitName)
	}
	if err := systemdapi.StartUnit(session.Context(), tunedUnitName); err != nil {
		return fmt.Errorf("start %s through systemd D-Bus: %w", tunedUnitName, err)
	}
	return nil
}

func waitForTunedBus(session dbusclient.SystemSession) error {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		state, err := dbusclient.Tuned.BusNameState(session.Context())
		if err != nil {
			return err
		}
		if state.Active {
			return nil
		}
		select {
		case <-session.Context().Done():
			return session.Context().Err()
		case <-timer.C:
			return fmt.Errorf("TuneD D-Bus service did not appear after starting %s", tunedUnitName)
		case <-ticker.C:
		}
	}
}

func isTunedRunning(session dbusclient.SystemSession) (bool, error) {
	var running bool
	err := session.CallStore(tunedControlIface+".is_running", dbusclient.CallPolicy{}, nil, &running)
	return running, err
}

func activeProfile(session dbusclient.SystemSession) (string, error) {
	var profile string
	err := session.CallStore(tunedControlIface+".active_profile", dbusclient.CallPolicy{}, nil, &profile)
	return profile, err
}

func recommendedProfile(session dbusclient.SystemSession) (string, error) {
	var profile string
	err := session.CallStore(tunedControlIface+".recommend_profile", dbusclient.CallPolicy{}, nil, &profile)
	return profile, err
}

func readProfiles(session dbusclient.SystemSession) ([]apischema.TunedProfile, error) {
	var records []tunedProfileRecord
	if err := session.CallStore(tunedControlIface+".profiles2", dbusclient.CallPolicy{}, nil, &records); err == nil {
		return profilesFromRecords(records), nil
	}

	var names []string
	if err := session.CallStore(tunedControlIface+".profiles", dbusclient.CallPolicy{}, nil, &names); err != nil {
		return nil, err
	}
	profiles := make([]apischema.TunedProfile, 0, len(names))
	for _, name := range names {
		profiles = append(profiles, apischema.TunedProfile{Name: name})
	}
	return profiles, nil
}

func profilesFromRecords(records []tunedProfileRecord) []apischema.TunedProfile {
	profiles := make([]apischema.TunedProfile, 0, len(records))
	for _, record := range records {
		profiles = append(profiles, apischema.TunedProfile{
			Name:        record.Name,
			Description: utils.OptionalString(record.Description),
		})
	}
	return profiles
}

func switchProfile(session dbusclient.SystemSession, profile string) (bool, string, error) {
	call := session.Object().CallWithContext(session.Context(), tunedControlIface+".switch_profile", 0, profile)
	if call.Err != nil {
		return false, "", call.Err
	}
	return boolStringResult(call.Body)
}

func autoProfile(session dbusclient.SystemSession) (bool, string, error) {
	call := session.Object().CallWithContext(session.Context(), tunedControlIface+".auto_profile", 0)
	if call.Err != nil {
		return false, "", call.Err
	}
	return boolStringResult(call.Body)
}

func disableTuned(session dbusclient.SystemSession) (bool, error) {
	var ok bool
	err := session.CallStore(tunedControlIface+".disable", dbusclient.CallPolicy{}, nil, &ok)
	return ok, err
}

func startTunedTunings(session dbusclient.SystemSession) (bool, error) {
	var ok bool
	err := session.CallStore(tunedControlIface+".start", dbusclient.CallPolicy{}, nil, &ok)
	return ok, err
}

func ensureTunedProfile(session dbusclient.SystemSession) (string, error) {
	if active, err := activeProfile(session); err == nil && active != "" {
		return active, nil
	}

	recommended, err := recommendedProfile(session)
	if err != nil {
		return "", err
	}
	if recommended == "" {
		return "", fmt.Errorf("TuneD has no active or recommended profile")
	}

	ok, message, err := autoProfile(session)
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
	switch len(body) {
	case 2:
		var result tunedBoolStringResult
		if err := godbus.Store(body, &result.OK, &result.Message); err != nil {
			return false, "", err
		}
		return result.OK, result.Message, nil
	case 1:
		var result tunedBoolStringResult
		if err := godbus.Store(body, &result); err != nil {
			return false, "", err
		}
		return result.OK, result.Message, nil
	}
	return false, "", fmt.Errorf("unexpected TuneD result signature")
}

func markProfiles(profiles []apischema.TunedProfile, active, recommended string) {
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
