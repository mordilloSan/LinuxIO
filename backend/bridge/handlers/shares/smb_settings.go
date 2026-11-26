package shares

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// The settings of the SMB service as a whole (docs/shared-folders.md, "The
// SMB service"): the oldest protocol it speaks and whether it encrypts, in
// [global] of smb.conf, and whether the server shows up on its own in
// Finder (smbd announces itself through Avahi) and in Windows' Network
// (the WS-Discovery service, wsdd).

// Samba's names for the settings, and the older names it still reads, which
// are removed when the setting is written.
const (
	paramMinProtocol    = "server min protocol"
	paramMinProtocolOld = "min protocol"
	paramEncrypt        = "server smb encrypt"
	paramEncryptOld     = "smb encrypt"
	paramMDNS           = "multicast dns register"
)

// windowsDiscoveryUnits are the names the WS-Discovery service has: Debian
// 13 and Ubuntu 25.04 on, and the releases before them and Fedora.
var windowsDiscoveryUnits = []string{"wsdd-server.service", "wsdd.service"}

type windowsDiscoveryOps struct {
	// unit is the installed service's name, "" when there is none.
	unit   func(ctx context.Context) string
	active func(ctx context.Context, unit string) bool
	// enabled says the unit starts at boot.
	enabled func(ctx context.Context, unit string) bool
	enable  func(ctx context.Context, unit string) error
	start   func(ctx context.Context, unit string) error
	stop    func(ctx context.Context, unit string) error
	disable func(ctx context.Context, unit string) error
}

// windowsDiscovery is a var so tests can stand in for systemd.
var windowsDiscovery = windowsDiscoveryOps{
	unit: func(ctx context.Context) string {
		for _, unit := range windowsDiscoveryUnits {
			if state, err := systemd.GetUnitFileState(ctx, unit); err == nil && state != "" && state != "masked" {
				return unit
			}
		}
		return ""
	},
	active: func(ctx context.Context, unit string) bool {
		state, err := systemd.GetActiveState(ctx, unit)
		return err == nil && state == "active"
	},
	enabled: func(ctx context.Context, unit string) bool {
		state, err := systemd.GetUnitFileState(ctx, unit)
		return err == nil && (state == "enabled" || state == "enabled-runtime")
	},
	enable:  systemd.EnableUnit,
	start:   systemd.StartUnit,
	stop:    systemd.StopUnit,
	disable: systemd.DisableUnit,
}

// CheckWindowsDiscoveryAvailability reports whether the WS-Discovery service
// is installed. It backs the capability registry's entry.
func CheckWindowsDiscoveryAvailability(ctx context.Context) (bool, error) {
	if windowsDiscovery.unit(ctx) == "" {
		return false, errors.New("the WS-Discovery service is not installed (install wsdd-server, or wsdd)")
	}
	return true, nil
}

// paramKey is a parameter name as Samba compares them: without regard to
// case or to the spaces in it.
func paramKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), ""))
}

// globalEdit is one rewrite of [global]: the parameters to set, by Samba's
// spelling of their names, the ones written so far, the lines of the result
// and where [global] ends in them (after its last line that is not blank).
type globalEdit struct {
	params  map[string]*string
	byKey   map[string]string
	written map[string]bool
	out     []string
	end     int
}

// take writes the line of the parameter name at index at of the result,
// once, when it has a value, and says whether it did.
func (e *globalEdit) take(name string, at int) bool {
	value := e.params[name]
	if value == nil || e.written[name] {
		return false
	}
	e.out = slices.Insert(e.out, at, "   "+name+" = "+*value)
	e.written[name] = true
	return true
}

// finish adds, at the end of [global], the parameters it did not set.
func (e *globalEdit) finish() {
	for _, name := range slices.Sorted(maps.Keys(e.params)) {
		if e.take(name, e.end) {
			e.end++
		}
	}
}

// line takes one line of [global] that is no section header: one that sets
// a parameter of the edit is replaced where it stands, or dropped when the
// parameter is to go or was written already; any other is kept.
func (e *globalEdit) line(line string) {
	trimmed := strings.TrimSpace(line)
	comment := strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";")
	if m := keyValueRegex.FindStringSubmatch(line); !comment && m != nil {
		if name, ours := e.byKey[paramKey(m[1])]; ours {
			if e.take(name, len(e.out)) {
				e.end = len(e.out)
			}
			return
		}
	}
	e.out = append(e.out, line)
	if trimmed != "" {
		e.end = len(e.out)
	}
}

// setGlobalParams returns content, the text of smb.conf, with each of
// params set in [global]: a line that sets it, however it is spelt, is
// replaced where it stands, one given nil is removed, and a parameter the
// section does not set is added at its end. The section is made, first,
// when the file has none. Nothing else changes.
func setGlobalParams(content string, params map[string]*string) string {
	edit := globalEdit{params: params, byKey: make(map[string]string, len(params)), written: map[string]bool{}}
	for name := range params {
		edit.byKey[paramKey(name)] = name
	}
	inGlobal, sawGlobal := false, false
	for line := range strings.SplitSeq(content, "\n") {
		m := sectionRegex.FindStringSubmatch(strings.TrimSpace(line))
		switch {
		case m != nil:
			if inGlobal {
				edit.finish()
			}
			inGlobal = strings.EqualFold(m[1], "global")
			sawGlobal = sawGlobal || inGlobal
			edit.out = append(edit.out, line)
			edit.end = len(edit.out)
		case inGlobal:
			edit.line(line)
		default:
			edit.out = append(edit.out, line)
		}
	}
	if inGlobal {
		edit.finish()
	}
	if sawGlobal {
		return strings.Join(edit.out, "\n")
	}
	made := globalEdit{params: params, written: map[string]bool{}, out: []string{"[global]"}, end: 1}
	made.finish()
	section := strings.Join(made.out, "\n") + "\n"
	if content == "" {
		return section
	}
	return section + "\n" + content
}

// globalParam returns what [global] sets a parameter to, by its name or an
// older one, and "" when it sets none of them.
func globalParam(sections map[string]map[string]string, names ...string) string {
	for section, props := range sections {
		if !strings.EqualFold(section, "global") {
			continue
		}
		for _, name := range names {
			for key, value := range props {
				if paramKey(key) == paramKey(name) {
					return value
				}
			}
		}
	}
	return ""
}

// minProtocolOf reads Samba's value of the oldest protocol as LinuxIO
// offers it: SMB2 or SMB3 for every dialect of each (Samba's own default is
// the first of SMB2), and a value LinuxIO does not offer as it is written.
func minProtocolOf(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch {
	case value == "", strings.HasPrefix(value, "SMB2"):
		return "SMB2"
	case strings.HasPrefix(value, "SMB3"):
		return "SMB3"
	}
	return value
}

// encryptionOf reads Samba's value of "server smb encrypt", which it takes
// in the spellings of its signing settings: default is Samba's own, which
// encrypts for a client that asks, and so is a value Samba would not take.
func encryptionOf(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "no", "false", "0", "disabled":
		return "off"
	case "desired":
		return "desired"
	case "required", "mandatory", "force", "forced", "enforced":
		return "required"
	}
	return "default"
}

func sambaBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on":
		return true
	case "no", "false", "0", "off":
		return false
	}
	return fallback
}

// GetSMBSettings reads the settings of the SMB service.
func GetSMBSettings(ctx context.Context) (apischema.SMBSettings, error) {
	sections, err := parseSmbConf()
	if err != nil {
		return apischema.SMBSettings{}, err
	}
	unit := windowsDiscovery.unit(ctx)
	return apischema.SMBSettings{
		MinProtocol:      minProtocolOf(globalParam(sections, paramMinProtocol, paramMinProtocolOld)),
		Encryption:       encryptionOf(globalParam(sections, paramEncrypt, paramEncryptOld)),
		MacDiscovery:     sambaBool(globalParam(sections, paramMDNS), true),
		WindowsDiscovery: unit != "" && windowsDiscovery.active(ctx, unit),
	}, nil
}

// SetSMBSettings writes the settings of the SMB service and returns them as
// they then are. The protocol and the encryption are for connections made
// from then on, which a reload gives; whether smbd announces itself to Macs
// is read when it starts, so a change of that restarts it, and only that.
func SetSMBSettings(ctx context.Context, req apischema.SMBSettings) (apischema.SMBSettings, error) {
	if req.MinProtocol != "SMB2" && req.MinProtocol != "SMB3" {
		return apischema.SMBSettings{}, fmt.Errorf("%w: oldest protocol %q", bridgeipc.ErrInvalidArgs, req.MinProtocol)
	}
	if !slices.Contains([]string{"default", "desired", "required", "off"}, req.Encryption) {
		return apischema.SMBSettings{}, fmt.Errorf("%w: encryption %q", bridgeipc.ErrInvalidArgs, req.Encryption)
	}
	unit := windowsDiscovery.unit(ctx)
	if req.WindowsDiscovery && unit == "" {
		return apischema.SMBSettings{}, fmt.Errorf("%w: the WS-Discovery service is not installed", bridgeipc.ErrConflict)
	}
	if err := requireSambaServerAvailability(); err != nil {
		return apischema.SMBSettings{}, err
	}
	before, err := GetSMBSettings(ctx)
	if err != nil {
		return apischema.SMBSettings{}, err
	}
	mdns := "no"
	if req.MacDiscovery {
		mdns = "yes"
	}
	params := map[string]*string{
		paramMinProtocol:    &req.MinProtocol,
		paramMinProtocolOld: nil,
		paramEncrypt:        &req.Encryption,
		paramEncryptOld:     nil,
		paramMDNS:           &mdns,
	}
	if err = writeGlobalParams(ctx, params); err != nil {
		return apischema.SMBSettings{}, err
	}
	slog.Info("SMB settings saved", "min_protocol", req.MinProtocol, "encryption", req.Encryption, "mac_discovery", req.MacDiscovery, "windows_discovery", req.WindowsDiscovery)
	if err = reloadSamba(ctx); err != nil {
		return apischema.SMBSettings{}, err
	}
	if before.MacDiscovery != req.MacDiscovery {
		if err = restartSamba(ctx); err != nil {
			return apischema.SMBSettings{}, fmt.Errorf("the settings were saved, but Macs see the change only once the SMB service restarts: %w", err)
		}
	}
	if err = setWindowsDiscovery(ctx, unit, req.WindowsDiscovery); err != nil {
		return apischema.SMBSettings{}, err
	}
	return GetSMBSettings(ctx)
}

// writeGlobalParams sets params in [global] of smb.conf, under the lock of
// its other writers; a file that is not there yet is made.
func writeGlobalParams(ctx context.Context, params map[string]*string) error {
	if err := ensureSmbConfDir(); err != nil {
		return err
	}
	return withConfLock(ctx, func() error {
		content, err := os.ReadFile(smbConfFile)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to read %s: %w", smbConfFile, err)
		}
		if err = atomicfile.Write(smbConfFile, []byte(setGlobalParams(string(content), params)), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", smbConfFile, err)
		}
		return nil
	})
}

// setWindowsDiscovery has the WS-Discovery service run now and start at
// boot, or neither: each of the two is changed when it is not as wanted, so
// a service that runs but would not start at boot, or the reverse, ends as
// asked. With no service installed there is nothing to stop.
func setWindowsDiscovery(ctx context.Context, unit string, wanted bool) error {
	if unit == "" {
		return nil
	}
	active, enabled := windowsDiscovery.active(ctx, unit), windowsDiscovery.enabled(ctx, unit)
	type step struct {
		needed bool
		what   string
		do     func(context.Context, string) error
	}
	steps := []step{
		{wanted && !enabled, "enable", windowsDiscovery.enable},
		{wanted && !active, "start", windowsDiscovery.start},
		{!wanted && active, "stop", windowsDiscovery.stop},
		{!wanted && enabled, "disable", windowsDiscovery.disable},
	}
	changed := false
	for _, s := range steps {
		if !s.needed {
			continue
		}
		if err := s.do(ctx, unit); err != nil {
			return fmt.Errorf("%s %s: %w", s.what, unit, err)
		}
		changed = true
	}
	if changed {
		slog.Info("wsdd changed", "unit", unit, "shown", wanted)
	}
	return nil
}
