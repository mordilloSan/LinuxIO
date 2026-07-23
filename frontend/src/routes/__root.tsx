import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import type { LinuxIORouterContext } from "@/routes/-context";

import ErrorPage from "./-components/ErrorPage";
import NotFoundPage from "./-components/NotFoundPage";

export const Route = createRootRouteWithContext<LinuxIORouterContext>()({
  component: Outlet,
  errorComponent: RootError,
  notFoundComponent: RootNotFound,
});

function RootError() {
  return (
    <>
      <ErrorPage />
      <BootstrapLoaderReady />
    </>
  );
}

function RootNotFound() {
  return (
    <>
      <NotFoundPage />
      <BootstrapLoaderReady />
    </>
  );
}
