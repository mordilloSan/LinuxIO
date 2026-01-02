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
		// NOTE: Direct command execution is DISABLED for security
		// Commands must be defined in module YAML files
		// Use ExecCommandDirect() from module loader instead
		"exec": disabledExecHandler,
	}
}

// disabledExecHandler returns an error explaining that direct execution is disabled
func disabledExecHandler(args []string) (any, error) {
	return nil, fmt.Errorf("direct command execution is disabled - commands must be defined in module YAML files")
}

// ExecCommandDirect executes a command directly (used by module loader)
// This bypasses security checks and should only be called by whitelisted module handlers
func ExecCommandDirect(command, timeoutStr string) (any, error) {
	timeout := 10 * time.Second

	if timeoutStr != "" {
		if t, err := strconv.Atoi(timeoutStr); err == nil {
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
