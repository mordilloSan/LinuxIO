export {
  ASSISTANT_AGENTS,
  type AssistantAgentId,
  type AssistantConnection,
  type AssistantHandlers,
  type AuthStatus,
  connectAssistant,
  isAssistantAgentId,
  isAuthRequiredError,
  type SessionOpenResult,
  type SteerOutcome,
  textBlock,
} from "./connection";
export type { AssistantExit, AssistantReady } from "./transport";
