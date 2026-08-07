import { createFileRoute } from "@tanstack/react-router";

import { FileTextIcon } from "@/icons/svg";
import { loadRouteTransport } from "@/routes/-loader";

import GeneralLogsPage from "./-components/GeneralLogsPage";

export const Route = createFileRoute("/_authenticated/logs")({
  loader: ({ abortController, context }) =>
    loadRouteTransport(context, abortController.signal),
  component: GeneralLogsPage,
  staticData: {
    navigation: {
      icon: FileTextIcon,
      position: 35,
      title: "Logs",
    },
  },
});
