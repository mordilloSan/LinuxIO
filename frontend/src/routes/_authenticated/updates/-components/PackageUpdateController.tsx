import { createContext, useContext, type ReactNode } from "react";

import { usePackageUpdater } from "@/hooks/usePackageUpdater";

type PackageUpdateController = ReturnType<typeof usePackageUpdater>;

const PackageUpdateControllerContext =
  createContext<PackageUpdateController | null>(null);

export function PackageUpdateControllerProvider({
  children,
}: {
  children: ReactNode;
}) {
  const controller = usePackageUpdater();

  return (
    <PackageUpdateControllerContext value={controller}>
      {children}
    </PackageUpdateControllerContext>
  );
}

export function usePackageUpdateController(): PackageUpdateController {
  const controller = useContext(PackageUpdateControllerContext);
  if (!controller) {
    throw new Error("Package update controls must render inside UpdatesLayout");
  }
  return controller;
}
