import {
  lazyRouteComponent,
  type RouteComponent,
} from "@tanstack/react-router";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import useAuth from "@/hooks/useAuth";
import type { ProtectedRouteId } from "@/routing/protectedRouteCatalog";

type RouteModule = { default: RouteComponent };
type RouteImporter<TModule extends RouteModule = RouteModule> =
  () => Promise<TModule>;

function withRouteIcons<TModule extends RouteModule>(
  importer: RouteImporter<TModule>,
): RouteImporter<TModule> {
  return async () => {
    const [, routeModule] = await Promise.all([
      import("@/icons/icons"),
      importer(),
    ]);
    return routeModule;
  };
}

async function importAuthenticatedLayout(): Promise<RouteModule> {
  const [{ default: AuthenticatedRuntimeProvider }, { default: MainLayout }] =
    await Promise.all([
      import("@/contexts/AuthRuntimeProvider"),
      import("@/layouts/Main"),
    ]);

  return {
    default: function AuthenticatedLayout() {
      const { user } = useAuth();
      return (
        <AuthenticatedRuntimeProvider userId={user?.id}>
          <MainLayout />
        </AuthenticatedRuntimeProvider>
      );
    },
  };
}

async function importSignInScreen(): Promise<RouteModule> {
  const [{ default: AuthLayout }, { default: SignIn }] = await Promise.all([
    import("@/layouts/Auth"),
    import("@/pages/auth/Login"),
  ]);

  return {
    default: function SignInScreen() {
      return (
        <AuthLayout>
          <SignIn />
          <BootstrapLoaderReady />
        </AuthLayout>
      );
    },
  };
}

export const AuthenticatedLayout = lazyRouteComponent(
  importAuthenticatedLayout,
);
export const SignIn = lazyRouteComponent(importSignInScreen);
export const Page404 = lazyRouteComponent(() => import("@/pages/auth/Page404"));

export const protectedRouteComponents = {
  dashboard: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/dashboard")),
  ),
  network: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/network")),
  ),
  updates: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/updates")),
  ),
  services: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/services")),
  ),
  logs: lazyRouteComponent(withRouteIcons(() => import("@/pages/main/logs"))),
  storage: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/storage")),
  ),
  docker: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/docker")),
  ),
  vm: lazyRouteComponent(withRouteIcons(() => import("@/pages/main/vm"))),
  accounts: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/accounts")),
  ),
  shares: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/shares")),
  ),
  wireguard: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/wireguard")),
  ),
  hardware: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/hardware")),
  ),
  filebrowser: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/filebrowser")),
  ),
  terminal: lazyRouteComponent(
    withRouteIcons(() => import("@/pages/main/terminal")),
  ),
} as const satisfies Record<ProtectedRouteId, RouteComponent>;
