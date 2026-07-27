import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { render } from "@/test/render";

import RoutedTabContainer, { type RoutedTab } from "./RoutedTabContainer";

const tabs = [
  { label: "Users", to: "/accounts" },
  { label: "Groups", to: "/accounts/groups" },
] as const satisfies readonly RoutedTab[];

describe("RoutedTabContainer", () => {
  it("renders real links and tracks the active child URL", async () => {
    const rootRoute = createRootRoute({ component: Outlet });
    const accountsRoute = createRoute({
      component: () => (
        <RoutedTabContainer tabs={tabs}>
          <div>Route content</div>
        </RoutedTabContainer>
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
});
