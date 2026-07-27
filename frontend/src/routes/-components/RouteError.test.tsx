import { QueryClient, useSuspenseQuery } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import RouteError from "./RouteError";

describe("RouteError", () => {
  it("resets a failed loader query before invalidating the route", async () => {
    const query = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error("first route request failed"))
      .mockResolvedValue("Recovered route data");
    const queryOptions = {
      queryFn: query,
      queryKey: ["route-error-retry"],
      retry: false,
    };
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const rootRoute = createRootRouteWithContext<{
      queryClient: QueryClient;
    }>()({
      component: Outlet,
    });

    function RouteContent() {
      const { data } = useSuspenseQuery(queryOptions);
      return <div>{data}</div>;
    }

    const indexRoute = createRoute({
      component: RouteContent,
      getParentRoute: () => rootRoute,
      loader: ({ context }) =>
        context.queryClient.ensureQueryData(queryOptions),
      path: "/",
    });
    const router = createRouter({
      context: { queryClient },
      defaultErrorComponent: RouteError,
      history: createMemoryHistory({ initialEntries: ["/"] }),
      routeTree: rootRoute.addChildren([indexRoute]),
    });
    const { user } = render(
      <RouterProvider context={{ queryClient }} router={router} />,
      { queryClient },
    );

    expect(await screen.findByText("first route request failed")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByText("Recovered route data")).toBeVisible();
    expect(query).toHaveBeenCalledTimes(2);
  });
});
