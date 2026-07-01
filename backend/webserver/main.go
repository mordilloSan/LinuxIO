package main

import (
	"os"

	"github.com/mordilloSan/LinuxIO/backend/webserver/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args))
}
