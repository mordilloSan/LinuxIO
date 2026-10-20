import { createFileRoute, Outlet } from "@tanstack/react-router";

import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import useAuth from "@/hooks/useAuth";
import { HardDriveIcon } from "@/icons/svg";

import { STORAGE_TABS } from "./-components/storageTabs";

function StorageLayout() {
  const { privileged } = useAuth();
  const tabs: readonly RoutedTab[] = privileged
    ? STORAGE_TABS
    : STORAGE_TABS.filter((tab) => tab.to !== "/storage/btrfs");
  return (
    <RoutedTabLayout tabs={tabs}>
      <Outlet />
    </RoutedTabLayout>
  );
}

export const Route = createFileRoute("/_authenticated/storage")({
  component: StorageLayout,
  staticData: {
    navigation: {
      icon: HardDriveIcon,
      position: 40,
      title: "Storage",
    },
  },
});
