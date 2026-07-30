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
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        list_containers: {
          queryOptions: () => ({}),
        },
        start_all_stopped: {
          useJobAction: () => ({
            isPending: false,
            mutate: mocks.startAllStopped,
          }),
        },
        stop_container: {
          useJobAction: () => ({
            mutateAsync: mocks.stopContainer,
          }),
        },
        system_prune: {
          useJobAction: () => ({
            isPending: false,
            mutate: mocks.systemPrune,
          }),
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
