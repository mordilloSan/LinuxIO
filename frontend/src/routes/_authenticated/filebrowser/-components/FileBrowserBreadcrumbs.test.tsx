import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import FileBrowserBreadcrumbs from "./FileBrowserBreadcrumbs";

const routeMocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  splat: "srv/projects",
}));

vi.mock("@tanstack/react-router", () => ({
  getRouteApi: () => ({
    useNavigate: () => routeMocks.navigate,
    useParams: ({
      select,
    }: {
      select: (params: { _splat: string }) => unknown;
    }) => select({ _splat: routeMocks.splat }),
  }),
}));

describe("FileBrowserBreadcrumbs", () => {
  it("subscribes to the route path and navigates from the breadcrumb leaf", async () => {
    const { rerender, user } = render(<FileBrowserBreadcrumbs />);

    expect(
      screen.getByRole("button", { name: "breadcrumb-link-projects" }),
    ).toBeVisible();

    await user.click(
      screen.getByRole("button", { name: "breadcrumb-link-srv" }),
    );
    expect(routeMocks.navigate).toHaveBeenLastCalledWith({
      to: "/filebrowser/$",
      params: { _splat: "srv" },
      search: {},
    });

    routeMocks.splat = "srv/projects/archive";
    rerender(<FileBrowserBreadcrumbs />);

    expect(
      screen.getByRole("button", { name: "breadcrumb-link-archive" }),
    ).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Go to root" }));
    expect(routeMocks.navigate).toHaveBeenLastCalledWith({
      to: "/filebrowser/$",
      params: { _splat: "" },
      search: {},
    });
  });
});
