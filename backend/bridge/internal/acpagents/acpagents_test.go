package acpagents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupKnowsTheThreeAgents(t *testing.T) {
	for _, id := range []string{"claude", "gemini", "codex"} {
		agent, ok := Lookup(id)
		require.True(t, ok, id)
		require.Equal(t, "npx", agent.Command, id)
		require.NotEmpty(t, agent.Args, id)
	}
	_, ok := Lookup("opencode")
	require.False(t, ok)
	require.ElementsMatch(t, []string{"claude", "codex", "gemini"}, IDs())
}

func TestOverrideRestores(t *testing.T) {
	restore := Override("fake", Agent{Command: "/bin/true"})
	_, ok := Lookup("fake")
	require.True(t, ok)
	restore()
	_, ok = Lookup("fake")
	require.False(t, ok)
}
