//go:build ignore
// +build ignore

// This file is compiled and run only by `go generate`. It does NOT ship.
package main

import (
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mordilloSan/LinuxIO/backend/bridge/userconfig"
)

func main() {
	out := filepath.FromSlash("config_generated.yaml")

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	data, err := yaml.Marshal(userconfig.ExampleDefaults())
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	// rw-r--r--
	if err := os.WriteFile(out, data, 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}
}
