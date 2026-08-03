import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { act, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import {
  makeTabLayout,
  RoutedTabActions,
  RoutedTabLayout,
  type RoutedTab,
} from "./RoutedTabContainer";

const tabs = [
  { label: "Users", to: "/accounts" },
  { label: "Groups", to: "/accounts/groups" },
] as const satisfies readonly RoutedTab[];

const mockViewport = (initiallyMobile = true) => {
  let isMobile = initiallyMobile;
  const listeners = new Set<() => void>();
  const mediaQueryList = {
    addEventListener: (_type: string, listener: () => void) => {
      listeners.add(listener);
    },
    get matches() {
      return isMobile;
    },
    media: "(max-width: 599px)",
    removeEventListener: (_type: string, listener: () => void) => {
      listeners.delete(listener);
    },
  } as unknown as MediaQueryList;

  vi.spyOn(window, "matchMedia").mockReturnValue(mediaQueryList);

  return {
    setMobile(nextIsMobile: boolean) {
      isMobile = nextIsMobile;
      listeners.forEach((listener) => listener());
    },
  };
};

const NullableActions = () => {
  const [showAction, setShowAction] = useState(false);

  return (
    <>
      <RoutedTabActions>
        {showAction ? (
          <button type="button" onClick={() => setShowAction(false)}>
            Remove nullable action
          </button>
        ) : null}
      </RoutedTabActions>
      {!showAction ? (
        <button type="button" onClick={() => setShowAction(true)}>
          Show nullable action
        </button>
      ) : null}
    </>
  );
};

const BooleanActions = () => {
  // Kept in state so the `&&` collapses to a boolean child at runtime rather
  // than being folded away by the compiler (and flagged as constant by lint).
  const [showAction] = useState(false);

  return (
    <>
      <RoutedTabActions>
        {showAction && <button type="button">Hidden action</button>}
      </RoutedTabActions>
      <div>Boolean route content</div>
    </>
  );
};

const UsersActions = () => {
  const [clicks, setClicks] = useState(0);
  const [showActions, setShowActions] = useState(true);
  const [showPrimary, setShowPrimary] = useState(true);

  return (
    <>
      {showActions && showPrimary ? (
        <RoutedTabActions>
          <button type="button" onClick={() => setClicks((count) => count + 1)}>
            Users action {clicks}
          </button>
        </RoutedTabActions>
      ) : null}
      {showActions ? (
        <RoutedTabActions>
          <button type="button">Secondary action</button>
          <button type="button" onClick={() => setShowActions(false)}>
            Remove all actions
          </button>
        </RoutedTabActions>
      ) : (
        <button type="button" onClick={() => setShowActions(true)}>
          Restore actions
        </button>
      )}
      <button type="button" onClick={() => setShowPrimary(false)}>
        Remove primary action
      </button>
    </>
  );
};

const StatefulAction = () => {
  const [clicks, setClicks] = useState(0);

  return (
    <button type="button" onClick={() => setClicks((count) => count + 1)}>
      Stateful action {clicks}
    </button>
  );
};

describe("RoutedTabContainer", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("renders real links and tracks the active child URL", async () => {
    const rootRoute = createRootRoute({ component: Outlet });
    const accountsRoute = createRoute({
      component: () => (
        <RoutedTabLayout tabs={tabs}>
          <div>Route content</div>
        </RoutedTabLayout>
      ),
      getParentRoute: () => rootRoute,
      path: "accounts",
    });
    const groupsRoute = createRoute({
      getParentRoute: () => accountsRoute,
      path: "groups",
    });
    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/accounts"] }),
      routeTree: rootRoute.addChildren([
        accountsRoute.addChildren([groupsRoute]),
      ]),
    });
    await router.load();
    const { user } = render(<RouterProvider router={router} />);

    const users = await screen.findByRole("tab", { name: "Users" });
    const groups = screen.getByRole("tab", { name: "Groups" });
    expect(users).toHaveAttribute("href", "/accounts");
    expect(users).toHaveAttribute("aria-selected", "true");
    expect(groups).toHaveAttribute("href", "/accounts/groups");
    expect(groups).toHaveAttribute("aria-selected", "false");

    await user.click(groups);

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/accounts/groups"),
    );
    expect(groups).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("Route content")).toBeInTheDocument();
  });

  it("keeps a matchChildren tab selected while a nested detail route is open", async () => {
    const nestingTabs = [
      { label: "Users", to: "/accounts" },
      { label: "Groups", matchChildren: true, to: "/accounts/groups" },
    ] as const satisfies readonly RoutedTab[];
    const rootRoute = createRootRoute({ component: Outlet });
    const accountsRoute = createRoute({
      component: () => (
        <RoutedTabLayout tabs={nestingTabs}>
          <Outlet />
        </RoutedTabLayout>
      ),
      getParentRoute: () => rootRoute,
      path: "accounts",
    });
    const groupsRoute = createRoute({
      component: Outlet,
      getParentRoute: () => accountsRoute,
      path: "groups",
    });
    const groupDetailRoute = createRoute({
      component: () => <div>Group detail</div>,
      getParentRoute: () => groupsRoute,
      path: "detail",
    });
    const router = createRouter({
      history: createMemoryHistory({
        initialEntries: ["/accounts/groups/detail"],
      }),
      routeTree: rootRoute.addChildren([
        accountsRoute.addChildren([
          groupsRoute.addChildren([groupDetailRoute]),
        ]),
      ]),
    });
    await router.load();
    render(<RouterProvider router={router} />);

    expect(await screen.findByText("Group detail")).toBeInTheDocument();
    // The parent tab owns the detail route, so its pill stays selected...
    expect(screen.getByRole("tab", { name: "Groups" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    // ...while a sibling leaf tab stays exact-matched and unselected.
    expect(screen.getByRole("tab", { name: "Users" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
  });

  it("creates a route layout with the supplied container style", async () => {
    const AccountsLayout = makeTabLayout(tabs, {
      paddingInline: 0,
    });
    const rootRoute = createRootRoute({ component: Outlet });
    const accountsRoute = createRoute({
      component: AccountsLayout,
      getParentRoute: () => rootRoute,
      path: "accounts",
    });
    const usersRoute = createRoute({
      component: () => <div>Users route</div>,
      getParentRoute: () => accountsRoute,
      path: "/",
    });
    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/accounts"] }),
      routeTree: rootRoute.addChildren([
        accountsRoute.addChildren([usersRoute]),
      ]),
    });
    await router.load();
    const { container } = render(<RouterProvider router={router} />);

    expect(await screen.findByText("Users route")).toBeInTheDocument();
    expect(container.querySelector(".tab-container")).toHaveStyle({
      paddingInline: "0",
    });
  });

  it("does not register a mobile action slot for null children", async () => {
    mockViewport();
    const rootRoute = createRootRoute({ component: Outlet });
    const accountsRoute = createRoute({
      component: () => (
        <RoutedTabLayout tabs={tabs}>
          <Outlet />
        </RoutedTabLayout>
      ),
      getParentRoute: () => rootRoute,
      path: "accounts",
    });
    const usersRoute = createRoute({
      component: NullableActions,
      getParentRoute: () => accountsRoute,
      path: "/",
    });
    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/accounts"] }),
      routeTree: rootRoute.addChildren([
        accountsRoute.addChildren([usersRoute]),
      ]),
    });
    await router.load();
    const { container, user } = render(<RouterProvider router={router} />);

    await screen.findByRole("button", { name: "Show nullable action" });
    expect(container.querySelector(".app-icon-btn")).not.toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Show nullable action" }),
    );
    await waitFor(() =>
      expect(container.querySelector(".app-icon-btn")).toBeInTheDocument(),
    );
    const actionsTrigger = screen.getByRole("button", { name: "Actions" });
    expect(actionsTrigger).toHaveAttribute("aria-expanded", "false");
    await user.click(actionsTrigger);
    expect(actionsTrigger).toHaveAttribute("aria-expanded", "true");
    await user.click(
      await screen.findByRole("button", { name: "Remove nullable action" }),
    );

    await waitFor(() =>
      expect(container.querySelector(".app-icon-btn")).not.toBeInTheDocument(),
    );
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("does not register a mobile action slot for boolean children", async () => {
    mockViewport();
    const rootRoute = createRootRoute({ component: Outlet });
    const accountsRoute = createRoute({
      component: () => (
        <RoutedTabLayout tabs={tabs}>
          <Outlet />
        </RoutedTabLayout>
      ),
      getParentRoute: () => rootRoute,
      path: "accounts",
    });
    const usersRoute = createRoute({
      component: BooleanActions,
      getParentRoute: () => accountsRoute,
      path: "/",
    });
    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/accounts"] }),
      routeTree: rootRoute.addChildren([
        accountsRoute.addChildren([usersRoute]),
      ]),
    });
    await router.load();
    const { container } = render(<RouterProvider router={router} />);

    await screen.findByText("Boolean route content");
    expect(container.querySelector(".app-icon-btn")).not.toBeInTheDocument();
  });

  it("preserves action-local state across breakpoint changes", async () => {
    const viewport = mockViewport(false);
    const rootRoute = createRootRoute({ component: Outlet });
    const accountsRoute = createRoute({
      component: () => (
        <RoutedTabLayout tabs={tabs}>
          <Outlet />
        </RoutedTabLayout>
      ),
      getParentRoute: () => rootRoute,
      path: "accounts",
    });
    const usersRoute = createRoute({
      component: () => (
        <RoutedTabActions>
          <StatefulAction />
        </RoutedTabActions>
      ),
      getParentRoute: () => accountsRoute,
      path: "/",
    });
    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/accounts"] }),
      routeTree: rootRoute.addChildren([
        accountsRoute.addChildren([usersRoute]),
      ]),
    });
    await router.load();
    const { container, user } = render(<RouterProvider router={router} />);

    await user.click(
      await screen.findByRole("button", { name: "Stateful action 0" }),
    );
    expect(
      screen.getByRole("button", { name: "Stateful action 1" }),
    ).toBeInTheDocument();

    act(() => viewport.setMobile(true));
    await waitFor(() =>
      expect(container.querySelector(".app-icon-btn")).toBeInTheDocument(),
    );
    await user.click(container.querySelector(".app-icon-btn")!);
    await user.click(
      await screen.findByRole("button", { name: "Stateful action 1" }),
    );
    expect(
      screen.getByRole("button", { name: "Stateful action 2" }),
    ).toBeInTheDocument();

    act(() => viewport.setMobile(false));
    await waitFor(() =>
      expect(container.querySelector(".app-icon-btn")).not.toBeInTheDocument(),
    );
    expect(
      screen.getByRole("button", { name: "Stateful action 2" }),
    ).toBeInTheDocument();
  });

  it("ports child actions into the persistent mobile parent tab menu", async () => {
    mockViewport();
    const rootRoute = createRootRoute({ component: Outlet });
    const accountsRoute = createRoute({
      component: () => (
        <RoutedTabLayout tabs={tabs}>
          <Outlet />
        </RoutedTabLayout>
      ),
      getParentRoute: () => rootRoute,
      path: "accounts",
    });
    const usersRoute = createRoute({
      component: UsersActions,
      getParentRoute: () => accountsRoute,
      path: "/",
    });
    const groupsRoute = createRoute({
      component: () => (
        <RoutedTabActions>
          <button type="button">Groups action</button>
        </RoutedTabActions>
      ),
      getParentRoute: () => accountsRoute,
      path: "groups",
    });
    const otherRoute = createRoute({
      component: () => <div>Other route</div>,
      getParentRoute: () => rootRoute,
      path: "other",
    });
    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/accounts"] }),
      routeTree: rootRoute.addChildren([
        accountsRoute.addChildren([usersRoute, groupsRoute]),
        otherRoute,
      ]),
    });
    await router.load();
    const { container, user } = render(<RouterProvider router={router} />);

    await waitFor(() =>
      expect(container.querySelector(".app-icon-btn")).toBeInTheDocument(),
    );
    await user.click(container.querySelector(".app-icon-btn")!);
    expect(await screen.findByRole("menu")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Users action 0" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Secondary action" }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Users action 0" }));
    await user.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("menu")).not.toBeInTheDocument(),
    );
    await user.click(container.querySelector(".app-icon-btn")!);
    expect(
      await screen.findByRole("button", { name: "Users action 1" }),
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Remove all actions" }),
    );
    await waitFor(() =>
      expect(container.querySelector(".app-icon-btn")).not.toBeInTheDocument(),
    );
    await user.click(screen.getByRole("button", { name: "Restore actions" }));
    await waitFor(() =>
      expect(container.querySelector(".app-icon-btn")).toBeInTheDocument(),
    );
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();

    await user.click(container.querySelector(".app-icon-btn")!);
    expect(
      await screen.findByRole("button", { name: "Secondary action" }),
    ).toBeInTheDocument();
    await user.keyboard("{Escape}");
    await user.click(
      screen.getByRole("button", { name: "Remove primary action" }),
    );
    expect(container.querySelector(".app-icon-btn")).toBeInTheDocument();
    await user.click(container.querySelector(".app-icon-btn")!);
    expect(
      await screen.findByRole("button", { name: "Secondary action" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Users action 1" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "Groups" }));
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Groups" })).toHaveAttribute(
        "aria-selected",
        "true",
      ),
    );
    await user.click(container.querySelector(".app-icon-btn")!);
    expect(
      await screen.findByRole("button", { name: "Groups action" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Secondary action" }),
    ).not.toBeInTheDocument();

    await router.navigate({ to: "/other" as never });
    await waitFor(() =>
      expect(screen.getByText("Other route")).toBeInTheDocument(),
    );
    expect(container.querySelector(".app-icon-btn")).not.toBeInTheDocument();
  });
});
