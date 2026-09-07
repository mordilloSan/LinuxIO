import { createFileRoute, Outlet } from "@tanstack/react-router";

import { ProcessIcon } from "@/icons/svg";

export const Route = createFileRoute("/_authenticated/processes")({
  component: ProcessesLayout,
  staticData: {
    navigation: {
      icon: ProcessIcon,
      position: 100,
      title: "Processes",
    },
  },
});

function ProcessesLayout() {
  return <Outlet />;
}
