import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import type { LinuxIORouterContext } from "@/routes/-auth";

import NotFoundPage from "./-components/NotFoundPage";

export const Route = createRootRouteWithContext<LinuxIORouterContext>()({
  component: Outlet,
  notFoundComponent: RootNotFound,
});

function RootNotFound() {
  return (
    <>
      <NotFoundPage />
      <BootstrapLoaderReady />
    </>
  );
}
