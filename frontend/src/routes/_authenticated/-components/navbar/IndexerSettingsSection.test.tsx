import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";
import { createTestQueryClient, render } from "@/test/render";

const mocks = vi.hoisted(() => ({
  config: {
    exclude_paths: ["/proc", "/dev"],
    include_network_mounts: false,
    interval: "1h0m0s",
  },
  setConfig: vi.fn(),
  setTimer: vi.fn(),
  startIndexer: vi.fn(),
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("@/hooks/backgroundTasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({ startIndexer: mocks.startIndexer }),
}));
vi.mock("@/hooks/backgroundTasks/useIsIndexing", () => ({
  useIsIndexing: () => false,
}));
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: (endpoint: { route: string }) => ({
      isPending: false,
      mutateAsync: async (request: Record<string, unknown>) => {
        if (endpoint.route === "indexer.set_config") {
          mocks.setConfig(request);
          return { config: { ...mocks.config, ...request } };
        }
        mocks.setTimer(request);
        return { interval: request.interval };
      },
    }),
  };
});

import IndexerSettingsSection from "./IndexerSettingsSection";

function renderSection(status = "idle") {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(linuxio.indexer.get_config.queryKey, {
    ...mocks.config,
  });
  queryClient.setQueryData(linuxio.indexer.get_status.queryKey, {
    database_size: 1024,
    last_indexed: "2026-08-29T00:00:00Z",
    num_dirs: 2,
    num_files: 3,
    running: false,
    status,
    total_size: 2048,
  });
  queryClient.setQueryData(
    linuxio.systemd.get_unit_info({
      unitName: "linuxio-indexer-index.timer",
    }).queryKey,
    { ActiveState: "active", SubState: "waiting" },
  );
  return render(<IndexerSettingsSection />, { queryClient });
}

describe("IndexerSettingsSection", () => {
  beforeEach(() => {
    mocks.setConfig.mockClear();
    mocks.setTimer.mockClear();
    mocks.startIndexer.mockClear();
  });

  it("saves the reduced scan policy and timer fields", async () => {
    const { user } = renderSection();
    const excludedPaths = screen.getByLabelText("Excluded paths");
    await user.clear(excludedPaths);
    await user.type(excludedPaths, "/srv/cache");
    await user.click(screen.getByLabelText("Include network mounts"));
    await user.clear(screen.getByLabelText("Timer interval"));
    await user.type(screen.getByLabelText("Timer interval"), "30m");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mocks.setConfig).toHaveBeenCalledWith({
        exclude_paths: ["/srv/cache"],
        include_network_mounts: true,
      });
      expect(mocks.setTimer).toHaveBeenCalledWith({ interval: "30m" });
    });
    expect(screen.queryByLabelText("Database path")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Listen address")).not.toBeInTheDocument();
  });

  it("prompts for a first full index before initialization", async () => {
    const { user } = renderSection("uninitialized");
    await user.click(screen.getByRole("button", { name: "Run full index" }));
    expect(mocks.startIndexer).toHaveBeenCalledOnce();
  });
});
