import { createFileRoute, Outlet } from "@tanstack/react-router";

import { HardDriveIcon } from "@/icons/svg";

export const Route = createFileRoute("/_authenticated/storage")({
  component: Outlet,
  staticData: {
    navigation: {
      icon: HardDriveIcon,
      position: 40,
      title: "Storage",
    },
  },
});
