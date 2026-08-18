import { createFileRoute, Outlet } from "@tanstack/react-router";

import { RoutedTabLayout } from "@/components/tabbar";
import { RefreshCcwIcon } from "@/icons/svg";

import { PackageUpdateControllerProvider } from "./-components/PackageUpdateController";
import { UPDATES_TABS } from "./-components/updatesTabs";

export const Route = createFileRoute("/_authenticated/updates")({
  component: UpdatesLayout,
  staticData: {
    navigation: {
      icon: RefreshCcwIcon,
      position: 20,
      title: "Updates",
    },
  },
});

function UpdatesLayout() {
  return (
    <PackageUpdateControllerProvider>
      <RoutedTabLayout tabs={UPDATES_TABS}>
        <Outlet />
      </RoutedTabLayout>
    </PackageUpdateControllerProvider>
  );
}
