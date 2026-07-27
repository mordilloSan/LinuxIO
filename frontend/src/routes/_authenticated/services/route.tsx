import { createFileRoute, Outlet } from "@tanstack/react-router";

import { ServerCogIcon } from "@/icons/svg";

export const Route = createFileRoute("/_authenticated/services")({
  component: Outlet,
  staticData: {
    navigation: {
      icon: ServerCogIcon,
      position: 30,
      title: "Services",
    },
  },
});
