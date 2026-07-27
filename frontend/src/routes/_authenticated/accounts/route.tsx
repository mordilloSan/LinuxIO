import { createFileRoute, Outlet } from "@tanstack/react-router";

import { UsersIcon } from "@/icons/svg";

export const Route = createFileRoute("/_authenticated/accounts")({
  component: Outlet,
  staticData: {
    navigation: {
      icon: UsersIcon,
      position: 60,
      title: "Accounts",
    },
  },
});
