package terminal

import (
	"fmt"
	"os/exec"
)

// ListContainerShells returns a list of available shells in a container.
func ListContainerShells(containerID string) ([]string, error) {
	shells := []string{"bash", "sh", "zsh", "ash", "dash"}
	available := []string{}
	for _, sh := range shells {
		cmd := exec.Command("docker", "exec", containerID, "which", sh)
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			available = append(available, sh)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no known shell found")
	}
	return available, nil
}
