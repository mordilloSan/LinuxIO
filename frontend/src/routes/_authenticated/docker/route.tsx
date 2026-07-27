import { createFileRoute, Outlet } from "@tanstack/react-router";

import type { AccessPolicy } from "@/hooks/useCapabilities";
import { DockerIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";

const access = {
  requiredCapabilities: ["dockerAvailable"],
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/docker")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  component: Outlet,
  staticData: {
    access,
    navigation: {
      icon: DockerIcon,
      position: 50,
      title: "Docker",
    },
  },
});
