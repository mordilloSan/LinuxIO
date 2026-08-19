import { createFileRoute } from "@tanstack/react-router";

import { SettingsIcon } from "@/icons/svg";
import { loadRouteTransport } from "@/routes/-loader";

import SettingsPage from "./-components/SettingsPage";
import { validateSettingsTab } from "./-components/settingsTabs";

export const Route = createFileRoute("/_authenticated/settings")({
  // The open tab lives in the URL so a settings link can point at one, and so
  // Back leaves the tab it was opened from the way the routed tab strips do.
  validateSearch: validateSettingsTab,
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
