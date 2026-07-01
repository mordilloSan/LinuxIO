package main

import (
	"os"

	"github.com/mordilloSan/LinuxIO/backend/bridge/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args))
}
