import { createFileRoute } from "@tanstack/react-router";

import { makeTabLayout } from "@/components/tabbar";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { DockerIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";

import { DOCKER_TABS } from "./-components/dockerTabs";

const access = {
  requiredCapabilities: ["dockerAvailable"],
} satisfies AccessPolicy;

const DockerLayout = makeTabLayout(DOCKER_TABS);

export const Route = createFileRoute("/_authenticated/docker")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  component: DockerLayout,
  staticData: {
    access,
    navigation: {
      icon: DockerIcon,
      position: 50,
      title: "Docker",
    },
  },
});
