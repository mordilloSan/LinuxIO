package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

func CommandHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		"exec": execCommand,
	}
}

func execCommand(args []string) (any, error) {
	// args[0] = command string
	// args[1] = timeout (optional, in seconds)

	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	command := args[0]
	timeout := 10 * time.Second

	if len(args) > 1 {
		if t, err := strconv.Atoi(args[1]); err == nil {
			timeout = time.Duration(t) * time.Second
		}
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return map[string]interface{}{
			"exitCode": exitCode,
			"stdout":   string(output),
			"error":    err.Error(),
		}, nil
	}

	result := string(output)

	// Try to parse as JSON if it looks like JSON
	var jsonResult interface{}
	if err := json.Unmarshal([]byte(result), &jsonResult); err == nil {
		return jsonResult, nil
	}

	// Return as plain string
	return map[string]interface{}{
		"exitCode": 0,
		"stdout":   result,
	}, nil
}
