import React, { lazy, useMemo } from "react";

import { AuthGuard } from "@/components/guards/AuthGuard";
import { GuestGuard } from "@/components/guards/GuestGuard";
import {
  type AccessContext,
  type AccessPolicy,
  hasAccessPolicy,
  useAccessContext,
} from "@/hooks/useCapabilities";
import {
  CpuIcon,
  DockerIcon,
  FileTextIcon,
  FolderIcon,
  HardDriveIcon,
  HomeIcon,
  NetworkIcon,
  RefreshCcwIcon,
  ServerCogIcon,
  ShareIcon,
  TerminalIcon,
  UsersIcon,
  WireguardIcon,
} from "@/icons/svg";

type LazyRouteModule<T extends React.ComponentType<any>> = { default: T };
type LazyRouteImporter<T extends React.ComponentType<any>> = () => Promise<
  LazyRouteModule<T>
>;
type PreloadableLazyRoute<T extends React.ComponentType<any>> =
  React.LazyExoticComponent<T> & {
    preload: LazyRouteImporter<T>;
  };

function lazyWithPreload<T extends React.ComponentType<any>>(
  importer: LazyRouteImporter<T>,
): PreloadableLazyRoute<T> {
  const Component = lazy(importer) as PreloadableLazyRoute<T>;
  Component.preload = importer;
  return Component;
}

// Lazy load layouts
const MainLayout = lazyWithPreload(() => import("@/layouts/Main"));
const AuthLayout = lazyWithPreload(() => import("@/layouts/Auth"));

// Lazy load core pages
const Default = lazyWithPreload(() => import("@/pages/main/dashboard"));
const Updates = lazyWithPreload(() => import("@/pages/main/updates"));
const Docker = lazyWithPreload(() => import("@/pages/main/docker"));
const Services = lazyWithPreload(() => import("@/pages/main/services"));
const Logs = lazyWithPreload(() => import("@/pages/main/logs"));
const Network = lazyWithPreload(() => import("@/pages/main/network"));
const Hardware = lazyWithPreload(() => import("@/pages/main/hardware"));
const Wireguard = lazyWithPreload(() => import("@/pages/main/wireguard"));
const TerminalPage = lazyWithPreload(() => import("@/pages/main/terminal"));
const Shares = lazyWithPreload(() => import("@/pages/main/shares"));
const FileBrowser = lazyWithPreload(() => import("@/pages/main/filebrowser"));
const StoragePage = lazyWithPreload(() => import("@/pages/main/storage"));
const AccountsPage = lazyWithPreload(() => import("@/pages/main/accounts"));

// Auth pages
const SignIn = lazyWithPreload(() => import("@/pages/auth/Login"));
const Page404 = lazyWithPreload(() => import("@/pages/auth/Page404"));

// ============================================================================
// Unified Route Configuration with Sidebar
// ============================================================================

export interface RouteWithSidebar extends AccessPolicy {
  path?: string;
  element?: React.ReactNode;
  preload?: () => Promise<unknown>;
  children?: RouteWithSidebar[];
  sidebar?: {
    title: string;
    icon: React.ElementType | string;
    position: number;
  };
}

const coreRoutes: RouteWithSidebar[] = [
  {
    path: "",
    element: <Default />,
    preload: Default.preload,
    sidebar: {
      title: "Dashboard",
      icon: HomeIcon,
      position: 0,
    },
  },
  {
    path: "network",
    element: <Network />,
    preload: Network.preload,
    sidebar: {
      title: "Network",
      icon: NetworkIcon,
      position: 10,
    },
  },
  {
    path: "updates",
    element: <Updates />,
    preload: Updates.preload,
    sidebar: {
      title: "Updates",
      icon: RefreshCcwIcon,
      position: 20,
    },
  },
  {
    path: "services",
    element: <Services />,
    preload: Services.preload,
    sidebar: {
      title: "Services",
      icon: ServerCogIcon,
      position: 30,
    },
  },
  {
    path: "logs",
    element: <Logs />,
    preload: Logs.preload,
    sidebar: {
      title: "Logs",
      icon: FileTextIcon,
      position: 35,
    },
  },
  {
    path: "storage",
    element: <StoragePage />,
    preload: StoragePage.preload,
    sidebar: {
      title: "Storage",
      icon: HardDriveIcon,
      position: 40,
    },
  },
  {
    path: "docker",
    element: <Docker />,
    preload: Docker.preload,
    requiredCapabilities: ["dockerAvailable"],
    sidebar: {
      title: "Docker",
      icon: DockerIcon,
      position: 50,
    },
  },
  {
    path: "accounts",
    element: <AccountsPage />,
    preload: AccountsPage.preload,
    sidebar: {
      title: "Accounts",
      icon: UsersIcon,
      position: 60,
    },
  },
  {
    path: "shares",
    element: <Shares />,
    preload: Shares.preload,
    sidebar: {
      title: "Shares",
      icon: ShareIcon,
      position: 70,
    },
  },
  {
    path: "wireguard",
    element: <Wireguard />,
    preload: Wireguard.preload,
    requiresPrivileged: true,
    sidebar: {
      title: "Wireguard",
      icon: WireguardIcon,
      position: 80,
    },
  },
  {
    path: "hardware",
    element: <Hardware />,
    preload: Hardware.preload,
    requiredCapabilities: ["lmSensorsAvailable"],
    sidebar: {
      title: "Hardware",
      icon: CpuIcon,
      position: 90,
    },
  },
  {
    path: "filebrowser/*",
    element: <FileBrowser />,
    preload: FileBrowser.preload,
    sidebar: {
      title: "Navigator",
      icon: FolderIcon,
      position: 100,
    },
  },
  {
    path: "terminal",
    element: <TerminalPage />,
    preload: TerminalPage.preload,
    sidebar: {
      title: "Terminal",
      icon: TerminalIcon,
      position: 110,
    },
  },
];

function buildProtectedRoutes(access: AccessContext) {
  return coreRoutes.filter((route) => hasAccessPolicy(route, access));
}

export function usePreloadProtectedRouteChunks() {
  const access = useAccessContext();

  return useMemo(() => {
    const preloaders = buildProtectedRoutes(access)
      .map((route) => route.preload)
      .filter((preload): preload is () => Promise<unknown> => Boolean(preload));

    return () => Promise.allSettled(preloaders.map((preload) => preload()));
  }, [access]);
}

// ============================================================================
// Route Builder Hook
// ============================================================================

export function useAppRoutes() {
  const access = useAccessContext();

  return useMemo(() => {
    const allProtectedRoutes = buildProtectedRoutes(access);

    return [
      // Protected app
      {
        path: "/",
        element: (
          <AuthGuard>
            <MainLayout />
          </AuthGuard>
        ),
        children: [...allProtectedRoutes, { path: "*", element: <Page404 /> }],
      },

      // Sign-in (public)
      {
        path: "/sign-in",
        element: <AuthLayout />,
        children: [
          {
            index: true,
            element: (
              <GuestGuard>
                <SignIn />
              </GuestGuard>
            ),
          },
        ],
      },
    ];
  }, [access]);
}

// ============================================================================
// Sidebar Items Extraction
// ============================================================================

export function useSidebarItems() {
  const access = useAccessContext();

  return useMemo(() => {
    const allRoutes = buildProtectedRoutes(access);

    // Convert to sidebar format and sort by position
    return allRoutes
      .filter((route) => route.sidebar)
      .sort(
        (a, b) =>
          (a.sidebar?.position ?? Number.MAX_SAFE_INTEGER) -
          (b.sidebar?.position ?? Number.MAX_SAFE_INTEGER),
      )
      .map((route) => ({
        href: `/${route.path ?? ""}`.replace("/*", ""), // Remove wildcard from path
        title: route.sidebar!.title,
        icon: route.sidebar!.icon,
      }));
  }, [access]);
}
