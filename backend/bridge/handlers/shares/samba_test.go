package shares

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReloadSambaUsesSmbcontrolWhenAvailable(t *testing.T) {
	origSmbcontrolReload := smbcontrolReload
	origSystemdReloadUnit := systemdReloadUnit
	t.Cleanup(func() {
		smbcontrolReload = origSmbcontrolReload
		systemdReloadUnit = origSystemdReloadUnit
	})

	smbcontrolCalls := 0
	systemdCalls := 0

	smbcontrolReload = func(context.Context) ([]byte, error) {
		smbcontrolCalls++
		return nil, nil
	}
	systemdReloadUnit = func(context.Context, string) error {
		systemdCalls++
		return nil
	}

	if err := reloadSamba(context.Background()); err != nil {
		t.Fatalf("reloadSamba() error = %v", err)
	}
	if smbcontrolCalls != 1 {
		t.Fatalf("smbcontrolReload calls = %d, want 1", smbcontrolCalls)
	}
	if systemdCalls != 0 {
		t.Fatalf("systemdReloadUnit calls = %d, want 0", systemdCalls)
	}
}

func TestReloadSambaFallsBackToSystemdUnits(t *testing.T) {
	origSmbcontrolReload := smbcontrolReload
	origSystemdReloadUnit := systemdReloadUnit
	t.Cleanup(func() {
		smbcontrolReload = origSmbcontrolReload
		systemdReloadUnit = origSystemdReloadUnit
	})

	var gotUnits []string

	smbcontrolReload = func(context.Context) ([]byte, error) {
		return []byte("smbcontrol failed"), errors.New("boom")
	}
	systemdReloadUnit = func(_ context.Context, name string) error {
		gotUnits = append(gotUnits, name)
		if name == "smb.service" {
			return nil
		}
		return errors.New("missing unit")
	}

	if err := reloadSamba(context.Background()); err != nil {
		t.Fatalf("reloadSamba() error = %v", err)
	}

	wantUnits := []string{"smbd.service", "smb.service"}
	if !reflect.DeepEqual(gotUnits, wantUnits) {
		t.Fatalf("systemd reload attempts = %v, want %v", gotUnits, wantUnits)
	}
}

func TestReloadSambaReturnsErrorWhenAllMethodsFail(t *testing.T) {
	origSmbcontrolReload := smbcontrolReload
	origSystemdReloadUnit := systemdReloadUnit
	t.Cleanup(func() {
		smbcontrolReload = origSmbcontrolReload
		systemdReloadUnit = origSystemdReloadUnit
	})

	var gotUnits []string

	smbcontrolReload = func(context.Context) ([]byte, error) {
		return []byte("smbcontrol failed"), errors.New("boom")
	}
	systemdReloadUnit = func(_ context.Context, name string) error {
		gotUnits = append(gotUnits, name)
		return errors.New("missing unit")
	}

	err := reloadSamba(context.Background())
	if err == nil {
		t.Fatal("reloadSamba() error = nil, want non-nil")
	}

	wantUnits := []string{"smbd.service", "smb.service", "samba.service"}
	if !reflect.DeepEqual(gotUnits, wantUnits) {
		t.Fatalf("systemd reload attempts = %v, want %v", gotUnits, wantUnits)
	}
}

// useTempSmbConf points smbConfFile at a fresh temporary location for the
// duration of the test and returns the file path.
func useTempSmbConf(t *testing.T) string {
	t.Helper()
	origFile := smbConfFile
	t.Cleanup(func() { smbConfFile = origFile })

	dir := filepath.Join(t.TempDir(), "samba")
	smbConfFile = filepath.Join(dir, "smb.conf")
	return smbConfFile
}

// stubSambaAvailable overrides the create/update/delete availability gate.
func stubSambaAvailable(t *testing.T, ok bool, err error) {
	t.Helper()
	orig := sambaServerAvailable
	t.Cleanup(func() { sambaServerAvailable = orig })
	sambaServerAvailable = func() (bool, error) { return ok, err }
}

func TestCreateSambaShareUnavailableLeavesNoOrphanDir(t *testing.T) {
	useTempSmbConf(t)
	stubSambaAvailable(t, false, errors.New("smbd not found (install samba)"))

	sharePath := filepath.Join(t.TempDir(), "share")
	err := CreateSambaShare(context.Background(), "Orphan", map[string]string{"path": sharePath})
	if err == nil {
		t.Fatal("CreateSambaShare() error = nil, want unavailable error")
	}
	if !strings.Contains(err.Error(), "smbd") {
		t.Fatalf("error = %q, want it to mention the missing smbd binary", err)
	}
	if _, statErr := os.Stat(sharePath); !os.IsNotExist(statErr) {
		t.Fatalf("share directory %s should not exist after a failed create (stat err = %v)", sharePath, statErr)
	}
	if _, statErr := os.Stat(smbConfFile); !os.IsNotExist(statErr) {
		t.Fatalf("smb.conf should not have been created when Samba is unavailable")
	}
}

func TestCreateSambaShareCreatesMissingConfAndAppends(t *testing.T) {
	confFile := useTempSmbConf(t)
	stubSambaAvailable(t, true, nil)

	origReload := smbcontrolReload
	t.Cleanup(func() { smbcontrolReload = origReload })
	reloadCalls := 0
	smbcontrolReload = func(context.Context) ([]byte, error) {
		reloadCalls++
		return nil, nil
	}

	sharePath := filepath.Join(t.TempDir(), "share")
	if err := CreateSambaShare(context.Background(), "Docs", map[string]string{
		"path":    sharePath,
		"comment": "shared docs",
	}); err != nil {
		t.Fatalf("CreateSambaShare() error = %v", err)
	}

	if reloadCalls != 1 {
		t.Fatalf("reload calls = %d, want 1", reloadCalls)
	}
	if info, err := os.Stat(sharePath); err != nil || !info.IsDir() {
		t.Fatalf("share directory %s not created (err = %v)", sharePath, err)
	}

	data, err := os.ReadFile(confFile)
	if err != nil {
		t.Fatalf("reading smb.conf: %v", err)
	}
	content := string(data)
	for _, want := range []string{"[Docs]", "path = " + sharePath, "comment = shared docs"} {
		if !strings.Contains(content, want) {
			t.Fatalf("smb.conf missing %q:\n%s", want, content)
		}
	}
}
