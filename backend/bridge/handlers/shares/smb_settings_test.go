package shares

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// Only [global] is touched: a line there is replaced whatever its spelling
// (Samba reads names without regard to case or spaces), a missing one is
// added at the end of the section, one given no value is removed, and the
// rest of the file, comments and other sections included, stays as it is.
//
//go:fix inline
func TestSetGlobalParams(t *testing.T) {
	conf := strings.Join([]string{
		"# Samba",
		"[global]",
		"   workgroup = WORKGROUP",
		"   Server Min Protocol = NT1",
		"   smb encrypt = off",
		"; a comment",
		"",
		"[Media]",
		"   path = /mnt/tank/Media",
		"   smb encrypt = required",
		"",
	}, "\n")
	got := setGlobalParams(conf, map[string]*string{
		"server min protocol":    new("SMB3"),
		"server smb encrypt":     new("required"),
		"smb encrypt":            nil,
		"multicast dns register": new("no"),
	})
	want := strings.Join([]string{
		"# Samba",
		"[global]",
		"   workgroup = WORKGROUP",
		"   server min protocol = SMB3",
		"; a comment",
		"   multicast dns register = no",
		"   server smb encrypt = required",
		"",
		"[Media]",
		"   path = /mnt/tank/Media",
		"   smb encrypt = required",
		"",
	}, "\n")
	if got != want {
		t.Errorf("setGlobalParams() =\n%s\nwant\n%s", got, want)
	}
}

func TestSetGlobalParamsWithoutAGlobalSection(t *testing.T) {
	got := setGlobalParams("[Media]\n   path = /srv/media\n", map[string]*string{"server min protocol": new("SMB2"), "smb encrypt": nil})
	want := "[global]\n   server min protocol = SMB2\n\n[Media]\n   path = /srv/media\n"
	if got != want {
		t.Errorf("setGlobalParams() =\n%q\nwant\n%q", got, want)
	}
	if got := setGlobalParams("", map[string]*string{"server min protocol": new("SMB2")}); got != "[global]\n   server min protocol = SMB2\n" {
		t.Errorf("of an empty file = %q", got)
	}
}

// discovery stands in for systemd and the Windows discovery service.
type discovery struct {
	unit    string
	active  bool
	enabled bool
	calls   []string
	reloads int
}

func setupSMBSettings(t *testing.T, global string) (*fakePool, *discovery) {
	t.Helper()
	pool := setupFolders(t)
	if err := os.WriteFile(smbConfFile, []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &discovery{unit: "wsdd-server.service"}
	orig := windowsDiscovery
	t.Cleanup(func() { windowsDiscovery = orig })
	say := func(what string) func(context.Context, string) error {
		return func(_ context.Context, unit string) error {
			d.calls = append(d.calls, what+" "+unit)
			return nil
		}
	}
	windowsDiscovery = windowsDiscoveryOps{
		unit:    func(context.Context) string { return d.unit },
		active:  func(context.Context, string) bool { return d.active },
		enabled: func(context.Context, string) bool { return d.enabled },
		enable:  say("enable"), start: say("start"), stop: say("stop"), disable: say("disable"),
	}
	smbcontrolReload = func(context.Context) ([]byte, error) { d.reloads++; return nil, nil }
	return pool, d
}

func TestGetSMBSettings(t *testing.T) {
	tests := []struct {
		name   string
		global string
		active bool
		want   apischema.SMBSettings
	}{
		{"Samba's defaults", "[global]\n   workgroup = W\n", false,
			apischema.SMBSettings{MinProtocol: "SMB2", Encryption: "default", MacDiscovery: true}},
		{"as set", "[global]\n   server min protocol = SMB3_11\n   server smb encrypt = required\n   multicast dns register = no\n", true,
			apischema.SMBSettings{MinProtocol: "SMB3", Encryption: "required", WindowsDiscovery: true}},
		{"older spellings", "[global]\n   min protocol = SMB2_10\n   smb encrypt = desired\n", false,
			apischema.SMBSettings{MinProtocol: "SMB2", Encryption: "desired", MacDiscovery: true}},
		// What LinuxIO does not offer is shown as it is written.
		{"a protocol LinuxIO does not offer", "[global]\n   server min protocol = NT1\n   server smb encrypt = if_required\n", false,
			apischema.SMBSettings{MinProtocol: "NT1", Encryption: "default", MacDiscovery: true}},
		{"no file", "", false,
			apischema.SMBSettings{MinProtocol: "SMB2", Encryption: "default", MacDiscovery: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, d := setupSMBSettings(t, tt.global)
			d.active = tt.active
			got, err := GetSMBSettings(context.Background())
			if err != nil || got != tt.want {
				t.Errorf("GetSMBSettings() = %+v, %v, want %+v", got, err, tt.want)
			}
		})
	}
}

func TestSetSMBSettings(t *testing.T) {
	pool, d := setupSMBSettings(t, "[global]\n   workgroup = W\n\n[plain]\n   path = /srv/plain\n")
	ctx := context.Background()
	want := apischema.SMBSettings{MinProtocol: "SMB3", Encryption: "required", MacDiscovery: true, WindowsDiscovery: true}
	d.active = false
	if _, err := SetSMBSettings(ctx, want); err != nil {
		t.Fatalf("SetSMBSettings() = %v", err)
	}
	conf := readFile(t, smbConfFile)
	for _, line := range []string{"server min protocol = SMB3", "server smb encrypt = required", "multicast dns register = yes", "[plain]", "workgroup = W"} {
		if !strings.Contains(conf, line) {
			t.Errorf("smb.conf lacks %q:\n%s", line, conf)
		}
	}
	// New connections follow a reload; nothing told to Macs changed, so
	// smbd is not restarted.
	if d.reloads != 1 || pool.restarts != 0 {
		t.Errorf("reloads = %d, restarts = %d, want 1 and 0", d.reloads, pool.restarts)
	}
	if wantCalls := []string{"enable wsdd-server.service", "start wsdd-server.service"}; !slices.Equal(d.calls, wantCalls) {
		t.Errorf("discovery calls = %v, want %v", d.calls, wantCalls)
	}
	d.active, d.enabled = true, true
	got, err := GetSMBSettings(ctx)
	if err != nil || got != want {
		t.Errorf("read back = %+v, %v, want %+v", got, err, want)
	}

	// Hidden from Macs: smbd announces itself when it starts, so it is
	// restarted; hidden from Windows: the service is stopped.
	d.calls, d.reloads = nil, 0
	want.MacDiscovery, want.WindowsDiscovery = false, false
	if _, err = SetSMBSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	if pool.restarts != 1 {
		t.Errorf("restarts = %d, want 1", pool.restarts)
	}
	if wantCalls := []string{"stop wsdd-server.service", "disable wsdd-server.service"}; !slices.Equal(d.calls, wantCalls) {
		t.Errorf("discovery calls = %v, want %v", d.calls, wantCalls)
	}
	if conf := readFile(t, smbConfFile); !strings.Contains(conf, "multicast dns register = no") {
		t.Errorf("smb.conf:\n%s", conf)
	}

	// Unchanged: the Windows service is left as it is.
	d.calls, d.active, d.enabled = nil, false, false
	if _, err = SetSMBSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 0 {
		t.Errorf("discovery calls = %v, want none", d.calls)
	}

	// Stopped now but enabled: off also keeps it from starting at boot.
	d.calls, d.active, d.enabled = nil, false, true
	if _, err = SetSMBSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	if wantCalls := []string{"disable wsdd-server.service"}; !slices.Equal(d.calls, wantCalls) {
		t.Errorf("off while enabled and stopped: calls = %v, want %v", d.calls, wantCalls)
	}
	// Running now but not enabled: on also has it start at boot.
	d.calls, d.active, d.enabled = nil, true, false
	want.WindowsDiscovery = true
	if _, err = SetSMBSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	if wantCalls := []string{"enable wsdd-server.service"}; !slices.Equal(d.calls, wantCalls) {
		t.Errorf("on while running and not enabled: calls = %v, want %v", d.calls, wantCalls)
	}
}

// Samba reads the encryption setting in the spellings of its signing
// settings; each is shown as the choice it is.
func TestEncryptionOf(t *testing.T) {
	for value, want := range map[string]string{
		"": "default", "default": "default", "auto": "default", "if_required": "default", "Yes": "default", "true": "default", "1": "default", "on": "default", "enabled": "default",
		"off": "off", "No": "off", "false": "off", "0": "off", "disabled": "off",
		"desired":  "desired",
		"required": "required", "mandatory": "required", "force": "required", "forced": "required", "enforced": "required",
		// What Samba itself would not take is its default.
		"sometimes": "default",
	} {
		if got := encryptionOf(value); got != want {
			t.Errorf("encryptionOf(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestSetSMBSettingsRefuses(t *testing.T) {
	ok := apischema.SMBSettings{MinProtocol: "SMB2", Encryption: "default", MacDiscovery: true}
	tests := []struct {
		name   string
		change func(*apischema.SMBSettings, *discovery)
		want   error
	}{
		{"a protocol LinuxIO does not offer", func(s *apischema.SMBSettings, _ *discovery) { s.MinProtocol = "NT1" }, bridgeipc.ErrInvalidArgs},
		{"an unknown encryption", func(s *apischema.SMBSettings, _ *discovery) { s.Encryption = "always" }, bridgeipc.ErrInvalidArgs},
		{"Windows discovery without its service", func(s *apischema.SMBSettings, d *discovery) { s.WindowsDiscovery, d.unit = true, "" }, bridgeipc.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, d := setupSMBSettings(t, "[global]\n   workgroup = W\n")
			before := readFile(t, smbConfFile)
			req := ok
			tt.change(&req, d)
			if _, err := SetSMBSettings(context.Background(), req); !errors.Is(err, tt.want) {
				t.Fatalf("SetSMBSettings() = %v, want %v", err, tt.want)
			}
			if readFile(t, smbConfFile) != before || d.reloads != 0 {
				t.Error("a refused request changed smb.conf or reloaded Samba")
			}
		})
	}
}
