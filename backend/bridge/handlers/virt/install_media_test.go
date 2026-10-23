package virt

import (
	"strings"
	"testing"
)

func TestValidateInstallMediaPathRejectsDirectory(t *testing.T) {
	_, err := validateInstallMediaPath(t.TempDir())

	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("validateInstallMediaPath error = %v, want directory rejection", err)
	}
}

func TestValidateInstallMediaPathRejectsNonISOFile(t *testing.T) {
	path := tempFile(t, "installer.img")

	_, err := validateInstallMediaPath(path)

	if err == nil || !strings.Contains(err.Error(), ".iso") {
		t.Fatalf("validateInstallMediaPath error = %v, want .iso rejection", err)
	}
}

func TestValidateInstallMediaPathAcceptsRegularISO(t *testing.T) {
	path := tempFile(t, "installer.iso")

	normalized, err := validateInstallMediaPath("  " + path + "  ")

	if err != nil {
		t.Fatalf("validateInstallMediaPath: %v", err)
	}
	if normalized != path {
		t.Fatalf("normalized path = %q, want %q", normalized, path)
	}
}
