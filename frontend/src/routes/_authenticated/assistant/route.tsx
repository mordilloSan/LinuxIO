import { createFileRoute } from "@tanstack/react-router";

import type { AccessPolicy } from "@/hooks/useCapabilities";
import { useAssistantSettings } from "@/hooks/useConfig";
import { AssistantIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";
import { loadRouteTransport } from "@/routes/-loader";

import AssistantPage from "./-components/AssistantPage";

const access = {
  requiredCapabilities: ["nodeAvailable"],
} satisfies AccessPolicy;

function AssistantRoute() {
  const { agent } = useAssistantSettings();
  return <AssistantPage agent={agent || null} />;
}

export const Route = createFileRoute("/_authenticated/assistant")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: ({ abortController, context }) =>
    loadRouteTransport(context, abortController.signal),
  component: AssistantRoute,
  staticData: {
    access,
    navigation: { icon: AssistantIcon, position: 115, title: "Assistant" },
  },
});
