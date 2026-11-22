package control

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

func stubUpdateLookup(t *testing.T, update string, err error) *[]string {
	t.Helper()
	var asked []string
	old := findUpdateByName
	t.Cleanup(func() { findUpdateByName = old })
	findUpdateByName = func(_ context.Context, name string) (string, error) {
		asked = append(asked, name)
		return update, err
	}
	return &asked
}

func TestVersionNamesThePendingLinuxIOUpdate(t *testing.T) {
	asked := stubUpdateLookup(t, "0.56.0-1", nil)

	got, err := handleVersion(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatalf("handleVersion: %v", err)
	}
	want := apischema.VersionResponse{CurrentVersion: version.Version, UpdateVersion: "0.56.0-1"}
	if got != want {
		t.Fatalf("version = %+v, want %+v", got, want)
	}
	if len(*asked) != 1 || (*asked)[0] != "linuxio" {
		t.Fatalf("looked up %q, want [linuxio]", *asked)
	}
}

// No update listed, no PackageKit, or a PackageKit that fails: the answer
// still carries the running version, and no update.
func TestVersionWithoutAnUpdate(t *testing.T) {
	for name, lookupErr := range map[string]error{
		"none listed":   nil,
		"no PackageKit": fmt.Errorf("get updates: %w", dbusclient.ErrPackageKitUnavailable),
		"lookup failed": errors.New("PackageKit transaction failed"),
	} {
		t.Run(name, func(t *testing.T) {
			stubUpdateLookup(t, "", lookupErr)

			got, err := handleVersion(context.Background(), apischema.NoRequest{})
			if err != nil {
				t.Fatalf("handleVersion: %v", err)
			}
			if got != (apischema.VersionResponse{CurrentVersion: version.Version}) {
				t.Fatalf("version = %+v, want only the running version", got)
			}
		})
	}
}
