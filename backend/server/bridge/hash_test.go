package bridge

import (
	"os"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

func TestValidateBridgeHash_NoEmbeddedHash(t *testing.T) {
	// When no hash is embedded, validation should be skipped
	orig := config.BridgeSHA256
	config.BridgeSHA256 = ""
	defer func() { config.BridgeSHA256 = orig }()

	// Create a temp file to act as the bridge binary
	f, err := os.CreateTemp("", "bridge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString("test binary content"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	// Should return nil (skip validation in dev mode)
	err = validateBridgeHash(f.Name())
	if err != nil {
		t.Errorf("expected nil error with empty hash (dev mode), got: %v", err)
	}
}

func TestValidateBridgeHash_HashMismatch(t *testing.T) {
	// Create temp file
	f, err := os.CreateTemp("", "bridge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString("test content"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	// Set wrong hash
	orig := config.BridgeSHA256
	config.BridgeSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	defer func() { config.BridgeSHA256 = orig }()

	err = validateBridgeHash(f.Name())
	if err == nil {
		t.Error("expected error for hash mismatch, got nil")
	}
}

func TestValidateBridgeHash_HashMatch(t *testing.T) {
	// Create temp file with known content
	f, err := os.CreateTemp("", "bridge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString("test content"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	// SHA256 of "test content"
	orig := config.BridgeSHA256
	config.BridgeSHA256 = "6ae8a75555209fd6c44157c0aed8016e763ff435a19cf186f76863140143ff72"
	defer func() { config.BridgeSHA256 = orig }()

	err = validateBridgeHash(f.Name())
	if err != nil {
		t.Errorf("expected nil error for matching hash, got: %v", err)
	}
}

func TestValidateBridgeHash_FileNotFound(t *testing.T) {
	orig := config.BridgeSHA256
	config.BridgeSHA256 = "somehash1234567890abcdef1234567890abcdef1234567890abcdef12345678"
	defer func() { config.BridgeSHA256 = orig }()

	err := validateBridgeHash("/nonexistent/path/to/bridge")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}
