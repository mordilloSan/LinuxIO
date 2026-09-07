package main

import (
	"os"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
