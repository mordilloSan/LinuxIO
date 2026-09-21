import { createFileRoute, Outlet } from "@tanstack/react-router";

import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import useAuth from "@/hooks/useAuth";
import { ServerCogIcon } from "@/icons/svg";

import { SERVICES_TABS } from "./-components/servicesTabs";

function ServicesLayout() {
  const { privileged } = useAuth();
  const tabs: readonly RoutedTab[] = privileged
    ? SERVICES_TABS
    : SERVICES_TABS.filter((tab) => tab.to !== "/services/schedules");
  return (
    <RoutedTabLayout tabs={tabs}>
      <Outlet />
    </RoutedTabLayout>
  );
}

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
