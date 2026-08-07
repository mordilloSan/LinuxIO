import { createFileRoute } from "@tanstack/react-router";

import { makeTabLayout } from "@/components/tabbar";
import { ServerCogIcon } from "@/icons/svg";

import { SERVICES_TABS } from "./-components/servicesTabs";

const ServicesLayout = makeTabLayout(SERVICES_TABS, { paddingInline: 0 });

export const Route = createFileRoute("/_authenticated/services")({
  component: ServicesLayout,
  staticData: {
    navigation: {
      icon: ServerCogIcon,
      position: 30,
      title: "Services",
    },
  },
});
