package main

import (
	"os"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
