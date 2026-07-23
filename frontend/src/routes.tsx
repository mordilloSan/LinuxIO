import { linuxio } from "@/api";
import { lazyWithPreload, withRouteIcons } from "@/routing/lazyWithPreload";
import {
  protectedRouteCatalog,
  type ProtectedRouteId,
} from "@/routing/protectedRouteCatalog";
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
const VirtualMachines = lazyWithPreload(
  withRouteIcons(() => import("@/pages/main/vm")),
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

const catalogRoute = (id: ProtectedRouteId) => {
  const catalogEntry = protectedRouteCatalog.find(
    (candidate) => candidate.id === id,
  );
  if (!catalogEntry) throw new Error(`Unknown protected route: ${id}`);
  const { id: _id, ...route } = catalogEntry;
  return route;
};

export const coreRoutes: RouteWithSidebar[] = [
  {
    ...catalogRoute("dashboard"),
    element: <Default />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [
      routeQuery(linuxio.system.get_health_summary),
      routeQuery(linuxio.system.get_host_info),
      routeQuery(linuxio.system.get_uptime),
      routeQuery(linuxio.system.get_server_time),
    ],
    preload: Default.preload,
  },
  {
    ...catalogRoute("network"),
    element: <Network />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [routeQuery(linuxio.network.get_network_info)],
    preload: Network.preload,
  },
  {
    ...catalogRoute("updates"),
    element: <Updates />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchDataWhen: (access) => access.packageKitAvailable === true,
    prefetchQueries: [routeQuery(linuxio.updates.get_updates_basic)],
    preload: Updates.preload,
  },
  {
    ...catalogRoute("services"),
    element: <Services />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [routeQuery(linuxio.systemd.list_services)],
    preload: Services.preload,
  },
  {
    ...catalogRoute("logs"),
    element: <Logs />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeOnly,
    preload: Logs.preload,
  },
  {
    ...catalogRoute("storage"),
    element: <StoragePage />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeOnly,
    preload: StoragePage.preload,
  },
  {
    ...catalogRoute("docker"),
    element: <Docker />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [routeQuery(linuxio.docker.list_containers)],
    preload: Docker.preload,
  },
  {
    ...catalogRoute("vm"),
    element: <VirtualMachines />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [routeQuery(linuxio.virt.list)],
    preload: VirtualMachines.preload,
  },
  {
    ...catalogRoute("accounts"),
    element: <AccountsPage />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeAndData,
    prefetchQueries: [
      routeQuery(linuxio.accounts.list_users),
      routeQuery(linuxio.accounts.list_groups),
    ],
    preload: AccountsPage.preload,
  },
  {
    ...catalogRoute("shares"),
    element: <Shares />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeOnly,
    preload: Shares.preload,
  },
  {
    ...catalogRoute("wireguard"),
    element: <Wireguard />,
    intentPreload: ROUTE_INTENT_PRELOAD.heavyRouteOnly,
    preload: Wireguard.preload,
  },
  {
    ...catalogRoute("hardware"),
    element: <Hardware />,
    intentPreload: ROUTE_INTENT_PRELOAD.routeOnly,
    preload: Hardware.preload,
  },
  {
    ...catalogRoute("filebrowser"),
    element: <FileBrowser />,
    intentPreload: ROUTE_INTENT_PRELOAD.heavyRouteOnly,
    preload: FileBrowser.preload,
  },
  {
    ...catalogRoute("terminal"),
    element: <TerminalPage />,
    intentPreload: ROUTE_INTENT_PRELOAD.heavyRouteOnly,
    preload: TerminalPage.preload,
  },
];
