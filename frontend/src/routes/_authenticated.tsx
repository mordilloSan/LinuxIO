import { createFileRoute } from "@tanstack/react-router";

import { CACHE_TTL_MS, linuxio } from "@/api";
import useAuth from "@/hooks/useAuth";
import { requireAuthentication } from "@/routes/-context";
import { loadRouteQueries } from "@/routes/-loader";

import NotFoundPage from "./-components/NotFoundPage";
import AuthenticatedRuntimeProvider from "./_authenticated/-components/AuthenticatedRuntimeProvider";
import MainLayout from "./_authenticated/-components/MainLayout";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context, location }) =>
    requireAuthentication(context, location),
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.control.version.queryOptions({
        staleTime: CACHE_TTL_MS.FIVE_MINUTES,
      }),
    ]),
  component: AuthenticatedLayout,
  notFoundComponent: NotFoundPage,
});

function AuthenticatedLayout() {
  const { user } = useAuth();

  return (
    <AuthenticatedRuntimeProvider userId={user?.id}>
      <MainLayout />
    </AuthenticatedRuntimeProvider>
  );
}
