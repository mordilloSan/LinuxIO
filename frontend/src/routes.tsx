import { linuxio } from "@/api";
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
import { lazyWithPreload, withRouteIcons } from "@/routing/lazyWithPreload";
import { routeQuery, ROUTE_INTENT_PRELOAD } from "@/routing/routeIntentPreload";
import type { RouteWithSidebar } from "@/routing/routeTypes";

// Layouts
export const MainLayout = lazyWithPreload(() => import("@/layouts/Main"));
export const AuthLayout = lazyWithPreload(() => import("@/layouts/Auth"));

// Protected pages
const Default = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/dashboard")),
);
const Updates = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/updates")),
);
const Docker = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/docker")),
);
const Services = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/services")),
);
const Logs = lazyWithPreload(withRouteIcons(() => import("@/pages/main/logs")));
const Network = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/network")),
);
const Hardware = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/hardware")),
);
const Wireguard = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/wireguard")),
);
const TerminalPage = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/terminal")),
);
const Shares = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/shares")),
);
const FileBrowser = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/filebrowser")),
);
const StoragePage = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/storage")),
);
const AccountsPage = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/accounts")),
);

// Public pages
export const SignIn = lazyWithPreload(() => import("@/pages/auth/Login"));
export const Page404 = lazyWithPreload(() => import("@/pages/auth/Page404"));

export const coreRoutes: RouteWithSidebar[] = [
  {
    path: "",
    element: <Default />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [
      routeQuery(linuxio.system.get_health_summary),
      routeQuery(linuxio.system.get_host_info),
      routeQuery(linuxio.system.get_uptime),
      routeQuery(linuxio.system.get_server_time),
    ],
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
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [routeQuery(linuxio.network.get_network_info)],
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
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchDataWhen: (access) => access.packageKitAvailable === true,
    prefetchQueries: [routeQuery(linuxio.updates.get_updates_basic)],
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
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [routeQuery(linuxio.systemd.list_services)],
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
    intentPreload: ROUTE_INTENT_PRELOAD.routeOnly,
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
    intentPreload: ROUTE_INTENT_PRELOAD.routeOnly,
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
    intentPreload: ROUTE_INTENT_PRELOAD.heavyRouteOnly,
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
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [
      routeQuery(linuxio.accounts.list_users),
      routeQuery(linuxio.accounts.list_groups),
    ],
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
    intentPreload: ROUTE_INTENT_PRELOAD.routeOnly,
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
    intentPreload: ROUTE_INTENT_PRELOAD.heavyRouteOnly,
    preload: Wireguard.preload,
    requiresPrivileged: true,
    requiredCapabilities: ["wireguardAvailable"],
    sidebar: {
      title: "Wireguard",
      icon: WireguardIcon,
      position: 80,
    },
  },
  {
    path: "hardware",
    element: <Hardware />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeOnly,
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
    intentPreload: ROUTE_INTENT_PRELOAD.heavyRouteOnly,
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
    intentPreload: ROUTE_INTENT_PRELOAD.heavyRouteOnly,
    preload: TerminalPage.preload,
    sidebar: {
      title: "Terminal",
      icon: TerminalIcon,
      position: 110,
    },
  },
];
