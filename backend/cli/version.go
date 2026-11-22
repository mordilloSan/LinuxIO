package main

import (
	"fmt"
	"io"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// showVersion prints the version of this build. The package installs every
// program from one build, so it is the version of all of them, and no other
// program is run or read for it.
func showVersion(w io.Writer) {
	fmt.Fprintf(w, "LinuxIO %s\n", version.Version)
}
