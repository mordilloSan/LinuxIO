package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

var versionExecCommand = exec.Command

func cliVersionLine() string {
	return fmt.Sprintf("LinuxIO CLI %s", version.Version)
}

func showVersion(args []string) {
	if len(args) == 1 && args[0] == "--self" {
		fmt.Println(cliVersionLine())
		return
	}

	fmt.Printf("\033[1mLinuxIO CLI - Manage LinuxIO services\033[0m\n")
	fmt.Println("\nInstalled components:")
	fmt.Printf("  %s\n", cliVersionLine())

	// Check linuxio-webserver
	out, err := versionExecCommand("linuxio-webserver", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-webserver: not found or error")
	}

	// Check linuxio-bridge
	out, err = versionExecCommand("linuxio-bridge", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-bridge: not found or error")
	}

	// Check linuxio-auth
	out, err = versionExecCommand("linuxio-auth", "version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("linuxio-auth: not found or error")
	}

	// Check linuxio-indexer
	out, err = versionExecCommand("linuxio-indexer", "--version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-indexer: not found or error")
	}

	// Check linuxio-monitoring
	out, err = versionExecCommand("linuxio-monitoring", "--version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-monitoring: not found or error")
	}
}
