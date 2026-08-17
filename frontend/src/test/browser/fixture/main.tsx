import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { lazy, StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";

import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import buildAppTheme, { AppThemeProvider } from "@/theme";

import "@/theme/variables.css";

const UsersPage = lazy(() => import("./routes/UsersPage"));
const GroupsPage = lazy(() => import("./routes/GroupsPage"));
const AccessibilityPage = lazy(() => import("./routes/AccessibilityPage"));
const ScrollingTabsPage = lazy(() => import("./routes/ScrollingTabsPage"));

const tabs = [
  { label: "Users", to: "/accounts" },
  { label: "Groups", to: "/accounts/groups" },
] as const satisfies readonly RoutedTab[];

const waitForPendingState = () =>
  new Promise<void>((resolve) => {
    window.setTimeout(resolve, 350);
  });

function RootLayout() {
  return (
    <AppThemeProvider value={buildAppTheme("DARK")}>
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
