import { createFileRoute } from "@tanstack/react-router";

import useAuth from "@/hooks/useAuth";
import { requireAuthentication } from "@/routes/-context";

import NotFoundPage from "./-components/NotFoundPage";
import AuthenticatedRuntimeProvider from "./_authenticated/-components/AuthenticatedRuntimeProvider";
import MainLayout from "./_authenticated/-components/MainLayout";

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context, location }) =>
    requireAuthentication(context, location),
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
