package version

// Component is one LinuxIO program whose version `linuxio version` and the
// webserver's /api/version report.
type Component struct {
	// Name is the key in /api/version and the prefix of the program's version
	// line ("<Name> <version>").
	Name string
	// Binary is the program's file name in BinDir.
	Binary string
	// Args make the program print its version line.
	Args []string
}

// Components lists every LinuxIO program that reports a version. A program
// reports its own version without being run: the CLI and the webserver each
// skip their own entry.
var Components = []Component{
	{Name: "LinuxIO CLI", Binary: "linuxio", Args: []string{"version", "--self"}},
	{Name: "LinuxIO Web Server", Binary: "linuxio-webserver", Args: []string{"version"}},
	{Name: "LinuxIO Bridge", Binary: "linuxio-bridge", Args: []string{"version"}},
	{Name: "LinuxIO Auth", Binary: "linuxio-auth", Args: []string{"version"}},
	{Name: "LinuxIO Indexer", Binary: "linuxio-indexer", Args: []string{"--version"}},
	{Name: "LinuxIO Monitoring", Binary: "linuxio-monitoring", Args: []string{"--version"}},
}
