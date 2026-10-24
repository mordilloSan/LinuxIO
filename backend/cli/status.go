package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

func runStatus(parent context.Context) int {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	units, err := systemd.ListUnitsWithPrefix(ctx, "linuxio")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to query systemd: %v\n", err)
		return 1
	}

	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })

	const header = "  UNIT                                      LOAD    ACTIVE   SUB      DESCRIPTION"
	maxWidth := len(header)
	type row struct {
		dot  string
		text string
	}
	rows := make([]row, 0, len(units))
	for _, u := range units {
		var dot string
		switch u.ActiveState {
		case "active":
			dot = "\033[32m●\033[0m"
		case "failed":
			dot = "\033[31m●\033[0m"
		default:
			dot = "○"
		}
		text := fmt.Sprintf("%-44s %-8s %-8s %-8s %s", u.Name, u.LoadState, u.ActiveState, u.SubState, u.Description)
		if len(text)+2 > maxWidth {
			maxWidth = len(text) + 2
		}
		rows = append(rows, row{dot: dot, text: text})
	}

	fmt.Printf("  \033[4m%s\033[0m\n", header+strings.Repeat(" ", maxWidth-len(header)))
	for _, r := range rows {
		fmt.Printf("%s %s\n", r.dot, r.text)
	}
	fmt.Printf("\n\033[1m%d loaded units listed.\033[0m\n", len(units))
	return 0
}
