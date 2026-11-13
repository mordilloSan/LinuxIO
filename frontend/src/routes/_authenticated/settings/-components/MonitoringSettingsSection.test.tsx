import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio, type MonitoringConfig, type MonitoringStatus } from "@/api";
import { createTestQueryClient, render } from "@/test/render";

const mocks = vi.hoisted(() => ({ setConfig: vi.fn() }));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (request: Record<string, unknown>) => {
      if (endpoint.route === "monitoring.set_config") {
        mocks.setConfig(request);
      }
    },
  });
  return {
    ...actual,
    linuxio: mockMutationHooks(actual.linuxio, useCallMutation),
    useCallMutation,
  };
});

import MonitoringSettingsSection from "./MonitoringSettingsSection";

const config: MonitoringConfig = {
  collector_interval: "1m0s",
  disk_usage_cache: "0s",
  history: "cpu,mem",
  history_intervals: {},
  history_retention: "720h0m0s",
  listeners: [{ address: ":45876", name: "lan", plugins: ["cpu"] }],
  smart_refresh_interval: "1h0m0s",
  version: 1,
};

const status: MonitoringStatus = {
  collector_interval: "1m0s",
  config: {
    collector_interval: "1m0s",
    history_intervals: {},
    history_plugins: ["cpu", "mem"],
    history_retention: "720h0m0s",
    path: "/etc/linuxio/monitoring/config.yaml",
    source: "loaded",
    version: 1,
  },
  data_dir: "/var/lib/linuxio/monitoring",
  db_path: "/var/lib/linuxio/monitoring/metrics.db",
  db_size_bytes: 12 * 1024 * 1024,
  listeners: [
    {
      active: true,
      address: "unix:/run/linuxio/monitoring/api.sock",
      apis: ["metrics"],
      effective_address: "/run/linuxio/monitoring/api.sock",
      name: "api",
    },
    {
      active: true,
      address: "unix:/run/linuxio/monitoring/control.sock",
      apis: ["commands"],
      effective_address: "/run/linuxio/monitoring/control.sock",
      name: "control",
    },
  ],
  retention: { "1m": "720h0m0s" },
  smart_refresh_interval: "1h0m0s",
  version: "0.28.0",
};

function renderSection() {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(linuxio.monitoring.get_config.queryKey, {
    ...config,
    listeners: config.listeners.map((listener) => ({ ...listener })),
  });
  queryClient.setQueryData(linuxio.monitoring.get_status.queryKey, status);
  return render(<MonitoringSettingsSection />, {
    capabilities: { monitoringAvailable: true },
    queryClient,
  });
}

describe("MonitoringSettingsSection", () => {
  beforeEach(() => {
    mocks.setConfig.mockClear();
  });

  it("saves the disk usage cache and the listener plugin selection", async () => {
    const { user } = renderSection();

    const diskUsageCache = screen.getByLabelText("Disk usage cache");
    await user.clear(diskUsageCache);
    await user.type(diskUsageCache, "15m");

    const plugins = screen.getByRole("combobox", { name: "Plugins" });
    await user.type(plugins, "mem");
    await user.keyboard("{Enter}");

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.setConfig).toHaveBeenCalledWith({
        disk_usage_cache: "15m",
        listeners: [
          { address: ":45876", name: "lan", plugins: ["cpu", "mem"] },
        ],
      }),
    );
  });

  it("omits untouched listeners from the patch", async () => {
    const { user } = renderSection();

    const diskUsageCache = screen.getByLabelText("Disk usage cache");
    await user.clear(diskUsageCache);
    await user.type(diskUsageCache, "30m");

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.setConfig).toHaveBeenCalledWith({
        disk_usage_cache: "30m",
      }),
    );
  });

  it("adds a listener row with a default name and the next free port", async () => {
    const { user } = renderSection();

    await user.click(screen.getByRole("button", { name: "Add listener" }));

    const names = screen.getAllByLabelText("Name");
    const ports = screen.getAllByLabelText("Port");
    expect(names).toHaveLength(2);
    expect(names[1]).toHaveValue("monitor");
    expect(ports[1]).toHaveValue(45877);

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.setConfig).toHaveBeenCalledWith({
        listeners: [
          { address: ":45876", name: "lan", plugins: ["cpu"] },
          { address: ":45877", name: "monitor", plugins: [] },
        ],
      }),
    );
  });

  it("rejects a port outside 1-65535", async () => {
    const { user } = renderSection();

    const port = screen.getByLabelText("Port");
    await user.clear(port);
    await user.type(port, "70000");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Use a port from 1 to 65535."),
    ).toBeInTheDocument();
    expect(mocks.setConfig).not.toHaveBeenCalled();
  });

  it("shows a hand-edited host:port address read-only", () => {
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(linuxio.monitoring.get_config.queryKey, {
      ...config,
      listeners: [{ address: "192.168.1.66:45876", name: "lan", plugins: [] }],
    });
    queryClient.setQueryData(linuxio.monitoring.get_status.queryKey, status);
    render(<MonitoringSettingsSection />, {
      capabilities: { monitoringAvailable: true },
      queryClient,
    });

    expect(screen.getByLabelText("Address")).toBeDisabled();
    expect(screen.queryByLabelText("Port")).toBeNull();
  });

  it("sends an explicit empty list when the last listener is removed", async () => {
    const { user } = renderSection();

    await user.click(screen.getByRole("button", { name: "Remove listener" }));

    expect(screen.getByText(/No listeners configured/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.setConfig).toHaveBeenCalledWith({ listeners: [] }),
    );
  });

  it("rejects a listener without a name and keeps the remote-commands toggle gone", async () => {
    const { user } = renderSection();

    expect(screen.queryByLabelText("Allow remote commands")).toBeNull();

    await user.clear(screen.getByLabelText("Name"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Required.")).toBeInTheDocument();
    expect(mocks.setConfig).not.toHaveBeenCalled();
  });

  it("saves history plugin toggles and per-plugin intervals", async () => {
    const { user } = renderSection();

    await user.click(screen.getByRole("button", { name: /History plugins/ }));
    await user.click(await screen.findByLabelText("containers"));
    await user.click(screen.getByLabelText("mem"));
    await user.type(screen.getByLabelText("containers interval"), "5m");

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.setConfig).toHaveBeenCalledWith({
        history: "cpu,containers",
        history_intervals: { containers: "5m" },
      }),
    );
  });

  it("rejects a history interval that is not a whole multiple of the collector interval", async () => {
    const { user } = renderSection();

    await user.click(screen.getByRole("button", { name: /History plugins/ }));
    await user.type(await screen.findByLabelText("mem interval"), "90s");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText(
        "Use a whole multiple of the collector interval (1m).",
      ),
    ).toBeInTheDocument();
    expect(mocks.setConfig).not.toHaveBeenCalled();
  });

  it("has no interval field for smart, which follows the SMART refresh", async () => {
    const { user } = renderSection();

    expect(
      screen.getByRole("button", { name: "History plugins: 2 of 14 enabled" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /History plugins/ }));

    expect(await screen.findByLabelText("smart")).toBeInTheDocument();
    expect(screen.queryByLabelText("smart interval")).toBeNull();
    expect(screen.queryByLabelText("processes")).toBeNull();
  });

  it("shows the database size on disk", () => {
    renderSection();

    expect(screen.getByText("12 MB")).toBeInTheDocument();
  });
});
