package version

import (
	"fmt"
	"io"
	"strings"
)

// Program is what a LinuxIO program other than linuxio says about itself.
// Each of them answers the same two requests the same way, --version and
// --help (or -h), so nobody has to learn a spelling per program. They take
// no "help" or "version" word, since a daemon starts with flags only, and no
// -v, which reads as --verbose beside a program that has that flag.
type Program struct {
	// Binary is the program's file name, such as "linuxio-monitoring".
	Binary string
	// About says what the program is, after "<Binary>: ".
	About string
	// StartedBy names what starts the program, after "Started by ".
	StartedBy string
	// Usage holds the command lines the program is started with. The flags
	// in them are the starter's: the help shows them so that someone reading
	// a unit file can tell what they mean, not to be typed.
	Usage []string
}

// Answer writes the version or the help to w when the first argument asks
// for one, and reports whether it did. args holds the arguments after the
// program's name.
func (p Program) Answer(args []string, w io.Writer) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "--version":
		fmt.Fprintf(w, "%s %s\n", p.name(), Version)
	case "--help", "-h":
		p.WriteHelp(w)
	default:
		return false
	}
	return true
}

// names holds the name that starts the version line of each Go program
// other than linuxio; linuxio-auth.c prints "LinuxIO Auth" itself.
var names = map[string]string{
	"linuxio-webserver":     "LinuxIO Web Server",
	"linuxio-bridge":        "LinuxIO Bridge",
	"linuxio-indexer":       "LinuxIO Indexer",
	"linuxio-monitoring":    "LinuxIO Monitoring",
	"linuxio-worker":        "LinuxIO Worker",
	"linuxio-docker-update": "LinuxIO Docker Updater",
	"linuxio-claude-relay":  "LinuxIO Claude Relay",
}

// name returns the name that starts the program's version line: its name in
// names, or its file name for a program the table does not hold.
func (p Program) name() string {
	if name, ok := names[p.Binary]; ok {
		return name
	}
	return p.Binary
}

// WriteHelp writes the help: what the program is, what starts it and how.
func (p Program) WriteHelp(w io.Writer) {
	fmt.Fprintf(w, "%s: %s\n", p.Binary, p.About)
	fmt.Fprintf(w, "Started by %s, not by hand. Manage LinuxIO with the linuxio command.\n", p.StartedBy)
	fmt.Fprintf(w, "\nIt is started as:\n  %s\n", strings.Join(p.Usage, "\n  "))
	fmt.Fprintln(w, "\nIt also answers --help, -h and --version.")
}
