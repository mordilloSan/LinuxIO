import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useLocation,
} from "@tanstack/react-router";
import { lazy, StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";

import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import buildAppTheme, { AppThemeProvider } from "@/theme";
import { installTabNavigationIntent } from "@/utils/tabNavigation";

// Register the app's generated icon sets so fixture pages draw icons
// synchronously, as production does; fetching them from the Iconify API at
// runtime lands mid-test and moves row geometry under the specs.
import "@/icons/icons";
import "@/icons/shell";
import "@/theme/variables.css";

installTabNavigationIntent();

const UsersPage = lazy(() => import("./routes/UsersPage"));
const GroupsPage = lazy(() => import("./routes/GroupsPage"));
const AccessibilityPage = lazy(() => import("./routes/AccessibilityPage"));
const ScrollingTabsPage = lazy(() => import("./routes/ScrollingTabsPage"));
const VirtualFileBrowserPage = lazy(
  () => import("./routes/VirtualFileBrowserPage"),
);
const VirtualGridPage = lazy(() => import("./routes/VirtualGridPage"));
const VirtualExpansionTablePage = lazy(
  () => import("./routes/VirtualExpansionTablePage"),
);
const StylingGalleryPage = lazy(() => import("./routes/StylingGalleryPage"));

const DARK_THEME = buildAppTheme("DARK");
const LIGHT_THEME = buildAppTheme("LIGHT");

const tabs = [
  { label: "Users", to: "/accounts" },
  { label: "Groups", to: "/accounts/groups" },
] as const satisfies readonly RoutedTab[];

const waitForPendingState = () =>
  new Promise<void>((resolve) => {
    window.setTimeout(resolve, 350);
  });

function RootLayout() {
  // The styling gallery is photographed in both schemes; everything else
  // runs dark. The provider writes the scheme to :root, so it is chosen here
  // rather than by nesting a second provider under this one.
  const { pathname } = useLocation();
  const theme = pathname.startsWith("/styling/light")
    ? LIGHT_THEME
    : DARK_THEME;
  return (
    <AppThemeProvider value={theme}>
      <Outlet />
    </AppThemeProvider>
  );
}

function AccountsLayout() {
  return (
    <RoutedTabLayout tabs={tabs}>
      <Suspense fallback={<div role="status">Loading route chunk…</div>}>
        <Outlet />
      </Suspense>
    </RoutedTabLayout>
  );
}

function PendingRoute() {
  return <div role="status">Loading child route…</div>;
}

function FailedRoute({ error }: { error: Error }) {
  return <div role="alert">Route failed: {error.message}</div>;
}

function NotFoundRoute() {
  return <div role="alert">Fixture page not found</div>;
}

const rootRoute = createRootRoute({ component: RootLayout });
const accountsRoute = createRoute({
  component: AccountsLayout,
  getParentRoute: () => rootRoute,
  path: "accounts",
});
const accessibilityRoute = createRoute({
  component: AccessibilityPage,
  getParentRoute: () => rootRoute,
  path: "accessibility",
});
const growingTabsRoute = createRoute({
  component: () => <ScrollingTabsPage panel="grow" />,
  getParentRoute: () => rootRoute,
  path: "scrolling-tabs/grow",
});
const fillingTabsRoute = createRoute({
  component: () => <ScrollingTabsPage panel="fill" />,
  getParentRoute: () => rootRoute,
  path: "scrolling-tabs/fill",
});
const cardTabsRoute = createRoute({
  component: () => <ScrollingTabsPage panel="cards" />,
  getParentRoute: () => rootRoute,
  path: "scrolling-tabs/cards",
});
const virtualExpansionRoute = createRoute({
  component: VirtualExpansionTablePage,
  getParentRoute: () => rootRoute,
  path: "tables/virtual-expansion",
});
const virtualFileBrowserRoute = createRoute({
  component: VirtualFileBrowserPage,
  getParentRoute: () => rootRoute,
  path: "filebrowser/virtual",
});
const virtualGridRoute = createRoute({
  component: VirtualGridPage,
  getParentRoute: () => rootRoute,
  path: "grids/virtual",
});
const stylingDarkRoute = createRoute({
  component: StylingGalleryPage,
  getParentRoute: () => rootRoute,
  path: "styling/dark",
});
const stylingLightRoute = createRoute({
  component: StylingGalleryPage,
  getParentRoute: () => rootRoute,
  path: "styling/light",
});
const accountsIndexRoute = createRoute({
  component: UsersPage,
  getParentRoute: () => accountsRoute,
  path: "/",
});
const groupsRoute = createRoute({
  component: GroupsPage,
  getParentRoute: () => accountsRoute,
  loader: waitForPendingState,
  pendingComponent: PendingRoute,
  pendingMinMs: 0,
  pendingMs: 0,
  path: "groups",
});
const failedRoute = createRoute({
  errorComponent: FailedRoute,
  getParentRoute: () => accountsRoute,
  loader: () => {
    throw new Error("fixture loader rejected");
  },
  path: "error",
});

const routeTree = rootRoute.addChildren([
  accountsRoute.addChildren([accountsIndexRoute, groupsRoute, failedRoute]),
  accessibilityRoute,
  growingTabsRoute,
  fillingTabsRoute,
  cardTabsRoute,
  virtualExpansionRoute,
  virtualFileBrowserRoute,
  virtualGridRoute,
  stylingDarkRoute,
  stylingLightRoute,
]);
const router = createRouter({
  defaultNotFoundComponent: NotFoundRoute,
  defaultPendingMinMs: 0,
  defaultPendingMs: 0,
  defaultPreload: "intent",
  defaultPreloadDelay: 50,
  history: createBrowserHistory(),
  routeTree,
});

const container = document.getElementById("root");
createRoot(container!).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
);
