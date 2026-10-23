package apischema

type TerminalOpenRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
	// Agent, when set, runs that ACP agent's login command (its registry
	// command plus Args) in the PTY instead of the interactive shell.
	Agent string   `json:"agent,omitempty"`
	Args  []string `json:"args,omitempty"`
}

type ContainerOpenRequest struct {
	ContainerID string `json:"containerId"`
	Shell       string `json:"shell"`
	Cols        int    `json:"cols"`
	Rows        int    `json:"rows"`
}
