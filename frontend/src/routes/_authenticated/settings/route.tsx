import { createFileRoute } from "@tanstack/react-router";

import { SettingsIcon } from "@/icons/svg";
import { loadRouteTransport } from "@/routes/-loader";

import SettingsPage from "./-components/SettingsPage";

export const Route = createFileRoute("/_authenticated/settings")({
  loader: ({ abortController, context }) =>
    loadRouteTransport(context, abortController.signal),
  component: SettingsPage,
  staticData: {
    navigation: {
      icon: SettingsIcon,
      position: 120,
      title: "Settings",
    },
  },
});
