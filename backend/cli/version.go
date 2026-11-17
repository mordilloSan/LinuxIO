package main

import (
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// readComponentVersions reads the versions the installer recorded; tests swap
// it.
var readComponentVersions = func() (map[string]string, error) {
	return version.ReadComponentVersions(version.ComponentsFile)
}

func cliVersionLine() string {
	return fmt.Sprintf("LinuxIO CLI %s", version.Version)
}

// showVersion prints this program's version and, without --self, the version
// of every other program as the installer recorded it. It runs none of them:
// several are installed for root only.
func showVersion(args []string) {
	if len(args) == 1 && args[0] == "--self" {
		fmt.Println(cliVersionLine())
		return
	}

	fmt.Printf("\033[1mLinuxIO CLI - Manage LinuxIO services\033[0m\n")
	fmt.Println("\nInstalled components:")

	// A record that cannot be read leaves every other program unrecorded.
	recorded, _ := readComponentVersions()
	for _, c := range version.Components {
		switch v, ok := recorded[c.Name]; {
		case c.Binary == "linuxio":
			fmt.Printf("  %s\n", cliVersionLine())
		case ok:
			fmt.Printf("  %s %s\n", c.Name, v)
		default:
			fmt.Printf("  %s: version not recorded\n", c.Binary)
		}
	}
}
