package version

import (
	"fmt"
	"os"
	"strings"
)

// Component is one LinuxIO program whose version `linuxio version` and the
// webserver's /api/version report.
type Component struct {
	// Name is the key in /api/version and the prefix of the program's version
	// line ("<Name> <version>").
	Name string
	// Binary is the program's file name in BinDir.
	Binary string
}

// Components lists every LinuxIO program, in the order `linuxio version`
// prints them. The installer runs each one as root and records its version
// line in ComponentsFile (write_components_file in install-linuxio-binaries.sh
// names the same programs, and installer-fixtures.sh compares the two lists).
// A program's version line starts with its name here (Program.Answer, and
// the same text in linuxio-auth.c).
var Components = []Component{
	{Name: "LinuxIO CLI", Binary: "linuxio"},
	{Name: "LinuxIO Web Server", Binary: "linuxio-webserver"},
	{Name: "LinuxIO Bridge", Binary: "linuxio-bridge"},
	{Name: "LinuxIO Auth", Binary: "linuxio-auth"},
	{Name: "LinuxIO Indexer", Binary: "linuxio-indexer"},
	{Name: "LinuxIO Monitoring", Binary: "linuxio-monitoring"},
	{Name: "LinuxIO Worker", Binary: "linuxio-worker"},
	{Name: "LinuxIO Docker Updater", Binary: "linuxio-docker-update"},
	{Name: "LinuxIO Claude Relay", Binary: "linuxio-claude-relay"},
}

// ComponentsFile holds the version line of each installed program, one per
// line, as the installer read them. The CLI and the webserver read this file
// and never run the programs: several are installed for root only, and
// neither of the two runs as root.
const ComponentsFile = ShareDir + "/components"

// ReadComponentVersions returns the version of each component recorded in the
// file at path, keyed by the component's name. A line that names no component
// is skipped, so a program added later does not break an older reader.
func ReadComponentVersions(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read component versions: %w", err)
	}
	versions := make(map[string]string, len(Components))
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		for _, c := range Components {
			if rest, ok := strings.CutPrefix(line, c.Name+" "); ok {
				if rest = strings.TrimSpace(rest); rest != "" {
					versions[c.Name] = rest
				}
				break
			}
		}
	}
	return versions, nil
}
