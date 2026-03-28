import React, { lazy, useMemo } from "react";

import { AuthGuard } from "@/components/guards/AuthGuard";
import { GuestGuard } from "@/components/guards/GuestGuard";
import { ConfigProvider } from "@/contexts/ConfigContext";
import { SidebarProvider } from "@/contexts/SidebarContext";
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
  MonitoringIcon,
  NetworkIcon,
  RefreshCcwIcon,
  ServerCogIcon,
  ShareIcon,
  TerminalIcon,
  UsersIcon,
  WireguardIcon,
} from "@/icons/svg";
import { ConfiguredAppThemeProvider } from "@/theme";

// Lazy load layouts
const MainLayout = lazy(() => import("@/layouts/Main"));
const AuthLayout = lazy(() => import("@/layouts/Auth"));

// Lazy load core pages
const Default = lazy(() => import("@/pages/main/dashboard"));
const Updates = lazy(() => import("@/pages/main/updates"));
const Docker = lazy(() => import("@/pages/main/docker"));
const Services = lazy(() => import("@/pages/main/services"));
const Logs = lazy(() => import("@/pages/main/logs"));
const Monitoring = lazy(() => import("@/pages/main/monitoring"));
const Network = lazy(() => import("@/pages/main/network"));
const Hardware = lazy(() => import("@/pages/main/hardware"));
const Wireguard = lazy(() => import("@/pages/main/wireguard"));
const TerminalPage = lazy(() => import("@/pages/main/terminal"));
const Shares = lazy(() => import("@/pages/main/shares"));
const FileBrowser = lazy(() => import("@/pages/main/filebrowser"));
const StoragePage = lazy(() => import("@/pages/main/storage"));
const AccountsPage = lazy(() => import("@/pages/main/accounts"));

// Auth pages
const SignIn = lazy(() => import("@/pages/auth/Login"));
const Page404 = lazy(() => import("@/pages/auth/Page404"));

// ============================================================================
// Unified Route Configuration with Sidebar
// ============================================================================

export interface RouteWithSidebar extends AccessPolicy {
  path?: string;
  element?: React.ReactNode;
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
    sidebar: {
      title: "Dashboard",
      icon: HomeIcon,
      position: 0,
    },
  },
  {
    path: "network",
    element: <Network />,
    sidebar: {
      title: "Network",
      icon: NetworkIcon,
      position: 10,
    },
  },
  {
    path: "updates",
    element: <Updates />,
    sidebar: {
      title: "Updates",
      icon: RefreshCcwIcon,
      position: 20,
    },
  },
  {
    path: "services",
    element: <Services />,
    sidebar: {
      title: "Services",
      icon: ServerCogIcon,
      position: 30,
    },
  },
  {
    path: "logs",
    element: <Logs />,
    sidebar: {
      title: "Logs",
      icon: FileTextIcon,
      position: 35,
    },
  },
  {
    path: "monitoring",
    element: <Monitoring />,
    requiredCapabilities: ["dockerAvailable"],
    sidebar: {
      title: "Monitoring",
      icon: MonitoringIcon,
      position: 36,
    },
  },
  {
    path: "storage",
    element: <StoragePage />,
    sidebar: {
      title: "Storage",
      icon: HardDriveIcon,
      position: 40,
    },
  },
  {
    path: "docker",
    element: <Docker />,
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
    sidebar: {
      title: "Accounts",
      icon: UsersIcon,
      position: 60,
    },
  },
  {
    path: "shares",
    element: <Shares />,
    sidebar: {
      title: "Shares",
      icon: ShareIcon,
      position: 70,
    },
  },
  {
    path: "wireguard",
    element: <Wireguard />,
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
    sidebar: {
      title: "Navigator",
      icon: FolderIcon,
      position: 100,
    },
  },
  {
    path: "terminal",
    element: <TerminalPage />,
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
            <ConfigProvider>
              <ConfiguredAppThemeProvider>
                <SidebarProvider>
                  <MainLayout />
                </SidebarProvider>
              </ConfiguredAppThemeProvider>
            </ConfigProvider>
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
