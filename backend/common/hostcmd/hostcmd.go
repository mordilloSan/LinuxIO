// Package hostcmd holds the helpers handler packages share when they run host
// administration commands.
package hostcmd

import (
	"fmt"
	"strings"
)

// OutputError wraps a failed command's error with its argv and the trimmed
// combined output, so the caller's message shows what the tool printed.
func OutputError(name string, args []string, output []byte, err error) error {
	if err == nil {
		return nil
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
}

// ShellQuote single-quotes s as one word for a POSIX sh command line.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
