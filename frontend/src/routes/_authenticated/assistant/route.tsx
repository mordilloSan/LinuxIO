import { createFileRoute } from "@tanstack/react-router";

import type { AccessPolicy } from "@/hooks/useCapabilities";
import { useAssistantSettings } from "@/hooks/useConfig";
import { AssistantIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";
import { loadRouteTransport } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import AssistantPage from "./-components/AssistantPage";
import { useChatAddress } from "./-components/useChatAddress";

const access = {
  requiredCapabilities: ["nodeAvailable"],
} satisfies AccessPolicy;

function AssistantRoute() {
  const { agent, defaults } = useAssistantSettings();
  const { chat } = Route.useSearch();
  useChatAddress(chat);
  return (
    <AssistantPage
      agent={agent || null}
      chatDefaults={agent ? defaults[agent] : undefined}
      chat={chat}
    />
  );
}

export const Route = createFileRoute("/_authenticated/assistant")({
  // Only a hint: the page opens it when the agent lists it, never otherwise.
  validateSearch: (search) => ({ ...optionalString(search, "chat") }),
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: ({ abortController, context }) =>
    loadRouteTransport(context, abortController.signal),
  component: AssistantRoute,
  staticData: {
    access,
    navigation: { icon: AssistantIcon, position: 115, title: "Assistant" },
  },
});
