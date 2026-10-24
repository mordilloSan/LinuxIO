import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio, type DockerDaemonConfig } from "@/api";
import { createTestQueryClient, render } from "@/test/render";

const mocks = vi.hoisted(() => ({
  applyStorage: vi.fn(),
  restart: vi.fn(),
  save: vi.fn(),
  restartRequired: false,
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  type StorageOptions = {
    success?: (result: unknown) => void;
    options?: { onSettled?: () => void };
  };
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        apply_daemon_storage: {
          useTaskStreamAction: (options: StorageOptions) => ({
            isPending: false,
            mutate: (request: Record<string, unknown>) => {
              mocks.applyStorage(request);
              options.success?.({ config: request, restart_required: false });
              options.options?.onSettled?.();
            },
          }),
        },
      },
    },
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
  data_root: "",
  image_store: "",
  live_restore: false,
  log_max_file: "",
  log_max_size: "",
  storage_driver: "",
  userland_proxy: true,
};

function renderSection(privileged = true, containerdImageStore = false) {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(linuxio.docker.get_daemon_config.queryKey, config);
  queryClient.setQueryData(linuxio.docker.get_docker_info.queryKey, {
    containerd_image_store: containerdImageStore,
    storage_driver: containerdImageStore ? "overlayfs" : "overlay2",
  });
  return render(<DockerDaemonSettingsSection />, {
    auth: { privileged },
    queryClient,
  });
}

describe("DockerDaemonSettingsSection", () => {
  beforeEach(() => {
    mocks.save.mockClear();
    mocks.applyStorage.mockClear();
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

  it("changes storage only after the user confirms stopping Docker", async () => {
    const { user } = renderSection();

    await user.type(screen.getByLabelText("Data root"), " /mnt/pool/docker/ ");
    expect(
      screen.getByText("Images may stay in /var/lib/containerd"),
    ).toBeInTheDocument();
    await user.click(screen.getByLabelText("Image store"));
    await user.click(
      screen.getByRole("option", { name: "Classic (all in the data root)" }),
    );
    expect(
      screen.queryByText("Images may stay in /var/lib/containerd"),
    ).not.toBeInTheDocument();
    await user.click(screen.getByLabelText("Storage driver"));
    expect(
      screen.queryByRole("option", { name: "overlayfs" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: "btrfs" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Stop Docker to change storage?"),
    ).toBeInTheDocument();
    expect(mocks.applyStorage).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: "Stop Docker and apply" }),
    );

    expect(mocks.applyStorage).toHaveBeenCalledWith({
      ...config,
      data_root: "/mnt/pool/docker",
      image_store: "classic",
      storage_driver: "btrfs",
    });
    expect(mocks.save).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(
        screen.queryByText("Stop Docker to change storage?"),
      ).not.toBeInTheDocument(),
    );
  });

  it("switches the driver list with the chosen image store", async () => {
    const { user } = renderSection();

    await user.click(screen.getByLabelText("Image store"));
    await user.click(screen.getByRole("option", { name: "containerd" }));
    await user.click(screen.getByLabelText("Storage driver"));

    expect(screen.getByRole("option", { name: "native" })).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "overlay2" }),
    ).not.toBeInTheDocument();
  });

  it("offers containerd snapshotters when the containerd image store is on", async () => {
    const { user } = renderSection(true, true);

    await user.click(screen.getByLabelText("Storage driver"));
    expect(
      screen.getByRole("option", { name: "Automatic (now overlayfs)" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "native" })).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "overlay2" }),
    ).not.toBeInTheDocument();
  });

  it("rejects a relative data root without saving", async () => {
    const { user } = renderSection();

    await user.type(screen.getByLabelText("Data root"), "pool/docker");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Use an absolute path other than /."),
    ).toBeInTheDocument();
    expect(mocks.save).not.toHaveBeenCalled();
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
