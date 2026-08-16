import { createFileRoute } from "@tanstack/react-router";

import { DockerUpdateOperationProvider } from "@/components/docker/DockerUpdateOperationProvider";
import { makeTabLayout } from "@/components/tabbar";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { DockerIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";

import { DOCKER_TABS } from "./-components/dockerTabs";
import { useDockerUpdateStatusRefresh } from "./-components/useDockerUpdateCheck";

const access = {
  requiredCapabilities: ["dockerAvailable"],
} satisfies AccessPolicy;

const DockerTabsLayout = makeTabLayout(DOCKER_TABS);

function DockerLayout() {
  useDockerUpdateStatusRefresh();
  return (
    <DockerUpdateOperationProvider>
      <DockerTabsLayout />
    </DockerUpdateOperationProvider>
  );
}

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
