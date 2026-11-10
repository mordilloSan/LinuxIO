import type { CapabilityKey } from "@/api/capabilities";

// Kept free of the ACP SDK so eager code (config hooks) can import it.
export type AssistantAgentId = "claude" | "gemini" | "codex";

export const ASSISTANT_AGENTS: ReadonlyArray<{
  id: AssistantAgentId;
  label: string;
  /** Capabilities that must be installed before the agent can start. */
  requires: readonly CapabilityKey[];
}> = [
  {
    id: "claude",
    label: "Claude Code",
    requires: ["claudeCodeAvailable", "assistantClaudeAvailable"],
  },
  {
    id: "gemini",
    label: "Gemini CLI",
    requires: ["assistantGeminiAvailable"],
  },
  { id: "codex", label: "Codex", requires: ["assistantCodexAvailable"] },
];

export function isAssistantAgentId(value: unknown): value is AssistantAgentId {
  return ASSISTANT_AGENTS.some((agent) => agent.id === value);
}
