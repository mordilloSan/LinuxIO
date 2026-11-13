import { ASSISTANT_AGENTS, type AssistantAgentId } from "@/api/acp/agents";
import { CAPABILITIES, type CapabilityKey } from "@/api/capabilities";
import {
  useCapabilitiesResponse,
  useCapabilityState,
} from "@/hooks/useCapabilities";

export interface MissingRequirement {
  key: CapabilityKey;
  label: string;
  reason: string;
}

/**
 * What the chosen agent still needs before it can start. Starting an agent
 * never installs anything, so callers start one only when `ready` is true:
 * every requirement confirmed by the capability scan. `missing` lists the
 * ones the scan found absent, with the scan's own reason when it gave one.
 */
export function useAssistantRequirements(agent: AssistantAgentId | "" | null) {
  const state = useCapabilityState();
  const response = useCapabilitiesResponse();
  const required =
    ASSISTANT_AGENTS.find((entry) => entry.id === agent)?.requires ?? [];
  const missing: MissingRequirement[] = CAPABILITIES.filter(
    (def) => required.includes(def.state) && state[def.state] === false,
  ).map((def) => ({
    key: def.state,
    label: def.label,
    reason: response[def.wire]?.error ?? def.reasonUnavailable,
  }));
  return {
    ready: Boolean(agent) && required.every((key) => state[key] === true),
    missing,
  };
}
