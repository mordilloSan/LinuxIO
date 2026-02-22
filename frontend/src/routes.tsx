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

const DockerSvgIcon = () => (
  <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
    <path d="M13.983 11.078h2.119a.186.186 0 00.186-.185V9.006a.186.186 0 00-.186-.186h-2.119a.185.185 0 00-.185.185v1.888c0 .102.083.185.185.185m-2.954-5.43h2.118a.186.186 0 00.186-.186V3.574a.186.186 0 00-.186-.185h-2.118a.185.185 0 00-.185.185v1.888c0 .102.082.185.185.185m0 2.716h2.118a.187.187 0 00.186-.186V6.29a.186.186 0 00-.186-.185h-2.118a.185.185 0 00-.185.185v1.887c0 .102.082.185.185.186m-2.93 0h2.12a.186.186 0 00.184-.186V6.29a.185.185 0 00-.185-.185H8.1a.185.185 0 00-.185.185v1.887c0 .102.083.185.185.186m-2.964 0h2.119a.186.186 0 00.185-.186V6.29a.185.185 0 00-.185-.185H5.136a.186.186 0 00-.186.185v1.887c0 .102.084.185.186.186m5.893 2.715h2.118a.186.186 0 00.186-.185V9.006a.186.186 0 00-.186-.186h-2.118a.185.185 0 00-.185.185v1.888c0 .102.082.185.185.185m-2.93 0h2.12a.185.185 0 00.184-.185V9.006a.185.185 0 00-.184-.186h-2.12a.185.185 0 00-.184.185v1.888c0 .102.083.185.185.185m-2.964 0h2.119a.185.185 0 00.185-.185V9.006a.185.185 0 00-.184-.186h-2.12a.186.186 0 00-.186.186v1.887c0 .102.084.185.186.185m-2.92 0h2.12a.185.185 0 00.184-.185V9.006a.185.185 0 00-.184-.186h-2.12a.185.185 0 00-.184.185v1.888c0 .102.082.185.185.185M23.763 9.89c-.065-.051-.672-.51-1.954-.51-.338.001-.676.03-1.01.087-.248-1.7-1.653-2.53-1.716-2.566l-.344-.199-.226.327c-.284.438-.49.922-.612 1.43-.23.97-.09 1.882.403 2.661-.595.332-1.55.413-1.744.42H.751a.751.751 0 00-.75.748 11.376 11.376 0 00.692 4.062c.545 1.428 1.355 2.48 2.41 3.124 1.18.723 3.1 1.137 5.275 1.137.983.003 1.963-.086 2.93-.266a12.248 12.248 0 003.823-1.389c.98-.567 1.86-1.288 2.61-2.136 1.252-1.418 1.998-2.997 2.553-4.4h.221c1.372 0 2.215-.549 2.68-1.009.309-.293.55-.65.707-1.046l.098-.288Z" />
  </svg>
);

const WireguardSvgIcon = () => (
  <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
    {/* Outer blob removed — only inner detail paths rendered so the design is filled against a transparent background */}
    <path d="M8.155 7.576c2.4-1.47 5.469-.571 6.618 1.638.218.419.246 1.063.108 1.503-.477 1.516-1.601 2.366-3.145 2.728.455-.39.817-.832.933-1.442a2.112 2.112 0 0 0-.364-1.677 2.14 2.14 0 0 0-2.465-.75c-.95.36-1.47 1.228-1.377 2.294.087.99.839 1.632 2.245 1.876-.21.111-.372.193-.53.281a5.113 5.113 0 0 0-1.644 1.43c-.143.192-.24.208-.458.075-2.827-1.729-3.009-6.067.078-7.956zM6.04 18.258c-.455.116-.895.286-1.359.438.227-1.532 2.021-2.943 3.539-2.782a3.91 3.91 0 0 0-.74 2.072c-.504.093-.98.155-1.44.272zM15.703 3.3c.448.017.898.01 1.347.02a2.324 2.324 0 0 1 .334.047 3.249 3.249 0 0 1-.34.434c-.16.15-.341.296-.573.069-.055-.055-.187-.042-.283-.044-.447-.005-.894-.02-1.34-.003a8.323 8.323 0 0 0-1.154.118c-.072.013-.178.25-.146.338.078.207.191.435.359.567.619.49 1.277.928 1.9 1.413.604.472 1.167.99 1.51 1.7.446.928.46 1.9.267 2.877-.322 1.63-1.147 2.98-2.483 3.962-.538.395-1.205.62-1.821.903-.543.25-1.1.465-1.644.712-.98.446-1.53 1.51-1.369 2.615.149 1.015 1.04 1.862 2.059 2.037 1.223.21 2.486-.586 2.785-1.83.336-1.397-.423-2.646-1.845-3.024l-.256-.066c.38-.17.708-.291 1.012-.458q.793-.437 1.558-.925c.15-.096.231-.096.36.014.977.846 1.56 1.898 1.724 3.187.27 2.135-.74 4.096-2.646 5.101-2.948 1.555-6.557-.215-7.208-3.484-.558-2.8 1.418-5.34 3.797-5.83 1.023-.211 1.958-.637 2.685-1.425.47-.508.697-.944.775-1.141a3.165 3.165 0 0 0 .217-1.158 2.71 2.71 0 0 0-.237-.992c-.248-.566-1.2-1.466-1.435-1.656l-2.24-1.754c-.079-.065-.168-.06-.36-.047-.23.016-.815.048-1.067-.018.204-.155.76-.38 1-.56-.726-.49-1.554-.314-2.315-.46.176-.328 1.046-.831 1.541-.888a7.323 7.323 0 0 0-.135-.822c-.03-.111-.154-.22-.263-.283-.262-.154-.541-.281-.843-.434a1.755 1.755 0 0 1 .906-.28 3.385 3.385 0 0 1 .908.088c.54.123.97.042 1.399-.324-.338-.136-.676-.26-1.003-.407a9.843 9.843 0 0 1-.942-.493c.85.118 1.671.437 2.54.32l.022-.118-2.018-.47c1.203-.11 2.323-.128 3.384.388.299.146.61.266.897.432.14.08.233.24.348.365.09.098.164.23.276.29.424.225.89.234 1.366.223l.01-.16c.479.15 1.017.702 1.017 1.105-.776 0-1.55-.003-2.325.004-.083 0-.165.061-.247.094.078.046.155.128.235.131z" />
  </svg>
);

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
      icon: DockerSvgIcon,
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
      icon: WireguardSvgIcon,
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
