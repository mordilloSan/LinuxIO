package shares

import (
	"context"
	"errors"
	"reflect"
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
