import { createFileRoute, Outlet } from "@tanstack/react-router";

import { ShareIcon } from "@/icons/svg";

export const Route = createFileRoute("/_authenticated/shares")({
  component: Outlet,
  staticData: {
    navigation: {
      icon: ShareIcon,
      position: 70,
      title: "Shares",
    },
  },
});
