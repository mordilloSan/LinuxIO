import type { CapabilityKey } from "@/api/capabilities";

// Kept free of the ACP SDK so eager code (config hooks) can import it.
export type AssistantAgentId = "claude" | "gemini" | "codex";

export const ASSISTANT_AGENTS: ReadonlyArray<{
  id: AssistantAgentId;
  label: string;
  /** Capabilities that must be installed before the agent can start. */
  requires: readonly CapabilityKey[];
  /** The agent has a plugin CLI the bridge can drive (assistant.*_plugin). */
  plugins: boolean;
}> = [
  {
    id: "claude",
    label: "Claude Code",
    requires: ["claudeCodeAvailable", "assistantClaudeAvailable"],
    plugins: true,
  },
  {
    id: "gemini",
    label: "Gemini CLI",
    requires: ["assistantGeminiAvailable"],
    plugins: false,
  },
  {
    id: "codex",
    label: "Codex",
    requires: ["assistantCodexAvailable"],
    plugins: false,
  },
];

export function isAssistantAgentId(value: unknown): value is AssistantAgentId {
  return ASSISTANT_AGENTS.some((agent) => agent.id === value);
}
