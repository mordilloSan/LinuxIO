import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import DockerDashboardPage from "./DockerDashboardPage";

const mocks = vi.hoisted(() => ({
  queryState: {
    data: [
      { Id: "running-id", Names: ["/running"], State: "running" },
      { Id: "stopped-id", Names: ["/stopped"], State: "exited" },
    ],
    isFetching: true,
  },
  startAllStopped: vi.fn(),
  stopContainer: vi.fn(),
  systemPrune: vi.fn(),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => mocks.queryState,
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: (endpoint: { route?: string }) => {
      if (endpoint.route === "docker.stop_container") {
        return { mutateAsync: mocks.stopContainer, isPending: false };
      }
      if (endpoint.route === "docker.start_all_stopped") {
        return { mutate: mocks.startAllStopped, isPending: false };
      }
      return { mutate: mocks.systemPrune, isPending: false };
    },
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        list_containers: {
          queryKey: ["linuxio", "docker", "list_containers"],
          queryFn: vi.fn(),
          ...(actual.linuxio.docker.list_containers as object),
        },
        start_all_stopped: {
          route: "docker.start_all_stopped",
        },
        stop_container: {
          route: "docker.stop_container",
        },
        system_prune: {
          route: "docker.system_prune",
        },
      },
    },
  };
});

vi.mock("./DockerDashboard", () => ({
  default: () => <div>Docker dashboard</div>,
}));

vi.mock("./useDockerUpdateCheck", () => ({
  useDockerUpdateCheck: () => ({ button: null }),
}));

describe("DockerDashboardPage", () => {
  beforeEach(() => {
    mocks.queryState.isFetching = true;
    mocks.startAllStopped.mockReset();
    mocks.stopContainer.mockReset();
    mocks.stopContainer.mockResolvedValue(undefined);
    mocks.systemPrune.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("blocks bulk container actions while the container list refetches", async () => {
    const { rerender, user } = render(<DockerDashboardPage />);

    expect(screen.getByRole("button", { name: "Start All" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Stop All" })).toBeDisabled();

    mocks.queryState.isFetching = false;
    rerender(<DockerDashboardPage />);

    const stopAll = screen.getByRole("button", { name: "Stop All" });
    expect(stopAll).toBeEnabled();
    await user.click(stopAll);

    expect(mocks.stopContainer).toHaveBeenCalledWith({
      containerId: "running-id",
    });
  });
});
