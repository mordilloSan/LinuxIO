import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";
import { createTestQueryClient, render } from "@/test/render";

const mocks = vi.hoisted(() => ({
  config: {
    db_auto_vacuum: "NONE",
    db_busy_timeout: "5s",
    db_conn_max_idle_time: "5m0s",
    db_journal_mode: "WAL",
    db_max_idle_conns: 2,
    db_max_open_conns: 4,
    db_path: "/var/lib/linuxio/indexer/indexer.db",
    db_stmt_cache_size: 100,
    db_synchronous: "NORMAL",
    entries_default_limit: 200,
    entries_max_limit: 200,
    exclude_paths: ["/proc", "/dev"],
    fresh_index: true,
    fts_search: true,
    idle_timeout: "2m0s",
    include_hidden: true,
    include_network_mounts: false,
    index_name: "root",
    index_path: "/",
    integrity_check: "full" as const,
    interval: "1h0m0s",
    keep_indexes: 1,
    listen_addr: "",
    search_default_limit: 100,
    search_max_limit: 100,
    socket_path: "/run/linuxio/indexer.sock",
  },
  restart: vi.fn(),
  setConfig: vi.fn(),
  startIndexer: vi.fn(),
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), info: vi.fn(), success: vi.fn() },
}));
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
    useCallMutation: (
      endpoint: { route: string },
      config?: { success?: () => void },
    ) => {
      if (endpoint.route === "indexer.set_config") {
        return {
          isPending: false,
          mutateAsync: async (patch: Partial<typeof mocks.config>) => {
            mocks.setConfig(patch);
            return {
              config: { ...mocks.config, ...patch },
              restart_required: true,
            };
          },
        };
      }
      if (endpoint.route === "indexer.set_timer_interval") {
        return {
          isPending: false,
          mutateAsync: async () => ({
            config: mocks.config,
            interval: mocks.config.interval,
            timer_unit: "linuxio-indexer-index.timer",
          }),
        };
      }
      return {
        isPending: false,
        mutate: (request: unknown) => {
          mocks.restart(request);
          config?.success?.();
        },
      };
    },
  };
});

import IndexerSettingsSection from "./IndexerSettingsSection";

function renderSection(ftsActive = true) {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(linuxio.indexer.get_config.queryKey, {
    ...mocks.config,
  });
  queryClient.setQueryData(linuxio.indexer.get_status.queryKey, {
    database_size: 1024,
    fts_active: ftsActive,
    num_dirs: 2,
    num_files: 3,
    running: false,
    shm_size: 0,
    status: "idle",
    total_entries: 5,
    total_indexes: 1,
    total_on_disk: 1024,
    total_size: 2048,
    wal_size: 0,
  });
  queryClient.setQueryData(
    linuxio.systemd.get_unit_info({
      unitName: "linuxio-indexer-index.timer",
    }).queryKey,
    { ActiveState: "active", SubState: "waiting" },
  );
  return render(<IndexerSettingsSection />, {
    capabilities: { indexerAvailable: true },
    queryClient,
  });
}

describe("IndexerSettingsSection actions", () => {
  beforeEach(() => {
    mocks.restart.mockClear();
    mocks.setConfig.mockClear();
    mocks.startIndexer.mockClear();
  });

  it("restarts the LinuxIO-prefixed service after a restart-bound change", async () => {
    const { user } = renderSection();

    const socketPath = screen.getByLabelText("Socket path");
    await user.clear(socketPath);
    await user.type(socketPath, "/run/linuxio/next-indexer.sock");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await user.click(
      await screen.findByRole("button", { name: "Restart indexer" }),
    );
    expect(mocks.restart).toHaveBeenCalledWith({
      serviceName: "linuxio-indexer.service",
    });
  });

  it("offers a full index when the configured FTS mode is not active", async () => {
    const { user } = renderSection(false);

    await user.click(screen.getByRole("button", { name: "Run full index" }));
    await waitFor(() => expect(mocks.startIndexer).toHaveBeenCalledOnce());
  });

  it("saves excluded paths as a list", async () => {
    const { user } = renderSection();

    const excludePaths = screen.getByLabelText("Excluded paths");
    await user.clear(excludePaths);
    await user.type(excludePaths, "/proc\n/dev\n/srv/cache");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.setConfig).toHaveBeenCalledWith(
        expect.objectContaining({
          exclude_paths: ["/proc", "/dev", "/srv/cache"],
        }),
      ),
    );
  });
});
