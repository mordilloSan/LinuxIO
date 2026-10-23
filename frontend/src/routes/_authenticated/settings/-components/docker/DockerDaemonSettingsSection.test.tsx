import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio, type DockerDaemonConfig } from "@/api";
import { createTestQueryClient, render } from "@/test/render";

const mocks = vi.hoisted(() => ({
  restart: vi.fn(),
  save: vi.fn(),
  restartRequired: false,
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: (
      endpoint: { route: string },
      options: { success?: (result: unknown) => void },
    ) => ({
      isPending: false,
      mutate: (request: Record<string, unknown>) => {
        if (endpoint.route === "docker.set_daemon_config") {
          mocks.save(request);
          options.success?.({
            config: request,
            restart_required: mocks.restartRequired,
          });
        }
        if (endpoint.route === "systemd.restart_service") {
          mocks.restart(request);
          options.success?.(undefined);
        }
      },
    }),
  };
});

import DockerDaemonSettingsSection from "./DockerDaemonSettingsSection";

const config: DockerDaemonConfig = {
  address_pool_base: "",
  address_pool_size: 0,
  live_restore: false,
  log_max_file: "",
  log_max_size: "",
  userland_proxy: true,
};

function renderSection(privileged = true) {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(linuxio.docker.get_daemon_config.queryKey, config);
  return render(<DockerDaemonSettingsSection />, {
    auth: { privileged },
    queryClient,
  });
}

describe("DockerDaemonSettingsSection", () => {
  beforeEach(() => {
    mocks.save.mockClear();
    mocks.restart.mockClear();
    mocks.restartRequired = false;
  });

  it("saves live restore and offers no restart when the daemon reloaded", async () => {
    const { user } = renderSection();

    await user.click(screen.getByRole("checkbox", { name: "Live restore" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith({
        ...config,
        live_restore: true,
      }),
    );
    expect(screen.queryByText("Restart required")).not.toBeInTheDocument();
  });

  it("shows the restart action after a restart-only change and restarts docker", async () => {
    mocks.restartRequired = true;
    const { user } = renderSection();

    await user.type(screen.getByLabelText("Max size"), "10m");
    await user.type(screen.getByLabelText("Max files"), "3");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith({
        ...config,
        log_max_file: "3",
        log_max_size: "10m",
      }),
    );
    await user.click(screen.getByRole("button", { name: "Restart Docker" }));
    expect(mocks.restart).toHaveBeenCalledWith({
      serviceName: "docker.service",
    });
    await waitFor(() =>
      expect(screen.queryByText("Restart required")).not.toBeInTheDocument(),
    );
  });

  it("rejects an address pool size outside the base prefix without saving", async () => {
    const { user } = renderSection();

    await user.type(screen.getByLabelText("Base"), "172.20.0.0/16");
    await user.type(screen.getByLabelText("Network size"), "8");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Use a prefix between 16 and 32."),
    ).toBeInTheDocument();
    expect(mocks.save).not.toHaveBeenCalled();
  });

  it("disables editing without a privileged session", () => {
    renderSection(false);

    expect(
      screen.getByRole("checkbox", { name: "Live restore" }),
    ).toBeDisabled();
    expect(screen.getByLabelText("Max size")).toBeDisabled();
    expect(screen.getByText(/Privileged mode is required/)).toBeInTheDocument();
  });
});
