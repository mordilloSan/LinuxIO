import { createFileRoute, Outlet } from "@tanstack/react-router";

import { RefreshCcwIcon } from "@/icons/svg";

export const Route = createFileRoute("/_authenticated/updates")({
  component: Outlet,
  staticData: {
    navigation: {
      icon: RefreshCcwIcon,
      position: 20,
      title: "Updates",
    },
  },
});
