import { Icon } from "@iconify/react";
import Cpu from "lucide-react/dist/esm/icons/cpu";
import FileText from "lucide-react/dist/esm/icons/file-text";
import Folder from "lucide-react/dist/esm/icons/folder";
import HardDrive from "lucide-react/dist/esm/icons/hard-drive";
import Home from "lucide-react/dist/esm/icons/home";
import NetworkIcon from "lucide-react/dist/esm/icons/network";
import Puzzle from "lucide-react/dist/esm/icons/puzzle";
import RefreshCcw from "lucide-react/dist/esm/icons/refresh-ccw";
import ServerCog from "lucide-react/dist/esm/icons/server-cog";
import Share2 from "lucide-react/dist/esm/icons/share-2";
import TerminalIcon from "lucide-react/dist/esm/icons/terminal";
import Users from "lucide-react/dist/esm/icons/users";
import React, { lazy, useMemo } from "react";

import { linuxio, CACHE_TTL_MS } from "@/api";
import { AuthGuard } from "@/components/guards/AuthGuard";
import { GuestGuard } from "@/components/guards/GuestGuard";
import { PrivilegedGuard } from "@/components/guards/PrivilegedGuard";
import type { ModuleInfo } from "@/types/module";
import { createModuleLazyComponent } from "@/utils/moduleLoader";

// Lazy load layouts
const MainLayout = lazy(() => import("@/layouts/Main"));
const AuthLayout = lazy(() => import("@/layouts/Auth"));

// Lazy load core pages
const Default = lazy(() => import("@/pages/main/dashboard"));
const Updates = lazy(() => import("@/pages/main/updates"));
const Docker = lazy(() => import("@/pages/main/docker"));
const Services = lazy(() => import("@/pages/main/services"));
const Logs = lazy(() => import("@/pages/main/logs"));
const Network = lazy(() => import("@/pages/main/network"));
const Hardware = lazy(() => import("@/pages/main/hardware"));
const Wireguard = lazy(() => import("@/pages/main/wireguard"));
const TerminalPage = lazy(() => import("@/pages/main/terminal"));
const Shares = lazy(() => import("@/pages/main/shares"));
const FileBrowser = lazy(() => import("@/pages/main/filebrowser"));
const ModulesPage = lazy(() => import("@/pages/main/modules"));
const StoragePage = lazy(() => import("@/pages/main/storage"));
const AccountsPage = lazy(() => import("@/pages/main/accounts"));

// Auth pages
const SignIn = lazy(() => import("@/pages/auth/Login"));
const Page404 = lazy(() => import("@/pages/auth/Page404"));

// ============================================================================
// Unified Route Configuration with Sidebar
// ============================================================================

export interface RouteWithSidebar {
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
      icon: Home,
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
      icon: RefreshCcw,
      position: 20,
    },
  },
  {
    path: "services",
    element: <Services />,
    sidebar: {
      title: "Services",
      icon: ServerCog,
      position: 30,
    },
  },
  {
    path: "logs",
    element: <Logs />,
    sidebar: {
      title: "Logs",
      icon: FileText,
      position: 35,
    },
  },
  {
    path: "storage",
    element: <StoragePage />,
    sidebar: {
      title: "Storage",
      icon: HardDrive,
      position: 40,
    },
  },
  {
    path: "docker",
    element: <Docker />,
    sidebar: {
      title: "Docker",
      icon: () => <Icon icon="fa-brands:docker" />,
      position: 50,
    },
  },
  {
    path: "accounts",
    element: <AccountsPage />,
    sidebar: {
      title: "Accounts",
      icon: Users,
      position: 60,
    },
  },
  {
    path: "shares",
    element: <Shares />,
    sidebar: {
      title: "Shares",
      icon: Share2,
      position: 70,
    },
  },
  {
    path: "wireguard",
    element: (
      <PrivilegedGuard>
        <Wireguard />
      </PrivilegedGuard>
    ),
    sidebar: {
      title: "Wireguard",
      icon: () => <Icon icon="cib:wireguard" width="48" height="48" />,
      position: 80,
    },
  },
  {
    path: "hardware",
    element: <Hardware />,
    sidebar: {
      title: "Hardware",
      icon: Cpu,
      position: 90,
    },
  },
  {
    path: "filebrowser/*",
    element: <FileBrowser />,
    sidebar: {
      title: "Navigator",
      icon: Folder,
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
  {
    path: "modules",
    element: (
      <PrivilegedGuard>
        <ModulesPage />
      </PrivilegedGuard>
    ),
    sidebar: {
      title: "Modules",
      icon: Puzzle,
      position: 120,
    },
  },
];

// ============================================================================
// Dynamic Module Route Creation
// ============================================================================

function createModuleRoute(module: ModuleInfo): RouteWithSidebar {
  const ModuleComponent = createModuleLazyComponent(module.componentUrl);

  // Remove leading slash if present
  const path = module.route.startsWith("/")
    ? module.route.slice(1)
    : module.route;

  // Parse icon - support Iconify string format or use default puzzle icon
  let icon: React.ElementType | string;
  if (module.icon && module.icon.includes(":")) {
    // Iconify format like "mdi:puzzle"
    icon = module.icon;
  } else {
    // Default to puzzle icon
    icon = Puzzle;
  }

  return {
    path,
    element: (
      <React.Suspense fallback={<div>Loading module...</div>}>
        <ModuleComponent />
      </React.Suspense>
    ),
    sidebar: {
      title: module.title,
      icon,
      position: module.position || 1000, // Default to end if no position
    },
  };
}

// ============================================================================
// Route Builder Hook
// ============================================================================

export function useAppRoutes() {
  const { data: modules } = linuxio.modules.get_modules.useQuery({
    staleTime: CACHE_TTL_MS.ONE_MINUTE,
    refetchOnMount: false,
  });

  return useMemo(() => {
    // Merge core routes with module routes
    const allProtectedRoutes = [...coreRoutes];

    if (modules && modules.length > 0) {
      modules.forEach((module) => {
        allProtectedRoutes.push(createModuleRoute(module));
      });
    }

    return [
      // Protected app
      {
        path: "/",
        element: (
          <AuthGuard>
            <MainLayout />
          </AuthGuard>
        ),
        children: allProtectedRoutes,
      },

      // Auth (public)
      {
        path: "*",
        element: <AuthLayout />,
        children: [
          {
            path: "sign-in",
            element: (
              <GuestGuard>
                <SignIn />
              </GuestGuard>
            ),
          },
          { path: "*", element: <Page404 /> },
        ],
      },
    ];
  }, [modules]);
}

// ============================================================================
// Sidebar Items Extraction
// ============================================================================

export function useSidebarItems() {
  const { data: modules } = linuxio.modules.get_modules.useQuery({
    staleTime: CACHE_TTL_MS.ONE_MINUTE,
    refetchOnMount: false,
  });

  return useMemo(() => {
    // Extract sidebar config from all routes
    const allRoutes = [...coreRoutes];

    if (modules && modules.length > 0) {
      modules.forEach((module) => {
        allRoutes.push(createModuleRoute(module));
      });
    }

    // Convert to sidebar format and sort by position
    return allRoutes
      .filter((route) => route.sidebar)
      .map((route) => ({
        href: `/${route.path}`.replace("/*", ""), // Remove wildcard from path
        title: route.sidebar!.title,
        icon: route.sidebar!.icon,
      }))
      .sort(
        (a: any, b: any) =>
          (a.sidebar?.position || 0) - (b.sidebar?.position || 0),
      );
  }, [modules]);
}

// Default export for backward compatibility
// This will be replaced when App.tsx is updated to use useAppRoutes()
const routes = [
  {
    path: "/",
    element: (
      <AuthGuard>
        <MainLayout />
      </AuthGuard>
    ),
    children: coreRoutes.map((route) => ({
      path: route.path,
      element: route.element,
    })),
  },
  {
    path: "*",
    element: <AuthLayout />,
    children: [
      {
        path: "sign-in",
        element: (
          <GuestGuard>
            <SignIn />
          </GuestGuard>
        ),
      },
      { path: "*", element: <Page404 /> },
    ],
  },
];

export default routes;
