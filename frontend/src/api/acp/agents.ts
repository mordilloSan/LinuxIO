// Kept free of the ACP SDK so eager code (config hooks) can import it.
export type AssistantAgentId = "claude" | "gemini" | "codex";

export const ASSISTANT_AGENTS: ReadonlyArray<{
  id: AssistantAgentId;
  label: string;
}> = [
  { id: "claude", label: "Claude Code" },
  { id: "gemini", label: "Gemini CLI" },
  { id: "codex", label: "Codex" },
];

export function isAssistantAgentId(value: unknown): value is AssistantAgentId {
  return ASSISTANT_AGENTS.some((agent) => agent.id === value);
}
