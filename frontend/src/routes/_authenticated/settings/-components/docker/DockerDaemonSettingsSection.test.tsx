import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio, type DockerDaemonConfig } from "@/api";
import { createTestQueryClient, render } from "@/test/render";

const mocks = vi.hoisted(() => ({
  applyStorage: vi.fn(),
  failWith: null as Error | null,
  // Holds the storage job until the test lets it end.
  hold: null as Promise<unknown> | null,
  progress: null as string | null,
  restart: vi.fn(),
  restartRequired: false,
  save: vi.fn(),
  // The jobs the host reports running, and the ones the section took up.
  tasks: [] as unknown[],
  watched: [] as string[],
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("sonner", () => ({ toast: mocks.toast }));
vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: vi.fn(async (route: string) => {
    if (route === "tasks.list") return mocks.tasks;
    throw new Error("the bridge did not answer");
  }),
}));

interface JobHandlers {
  onProgress?: (progress: unknown) => void;
  onSnapshot?: (snapshot: unknown) => void;
}

async function runStorageJob(taskId: string, handlers: JobHandlers) {
  handlers.onSnapshot?.({ id: taskId });
  if (mocks.progress) handlers.onProgress?.({ message: mocks.progress });
  if (mocks.hold) await mocks.hold;
  if (mocks.failWith) throw mocks.failWith;
}

vi.mock("@/api/linuxio-core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/linuxio-core")>()),
  mutationRequest: async (
    handler: string,
    command: string,
    request: Record<string, unknown>,
    handlers: JobHandlers,
  ) => {
    const route = `${handler}.${command}`;
    if (route === "docker.apply_daemon_storage") {
      mocks.applyStorage(request);
      await runStorageJob("task-1", handlers);
      return { config: request, restart_required: false };
    }
    if (route === "docker.set_daemon_config") {
      mocks.save(request);
      return { config: request, restart_required: mocks.restartRequired };
    }
    if (route === "systemd.restart_service") mocks.restart(request);
    return undefined;
  },
  watchMutation: async (taskId: string, handlers: JobHandlers) => {
    mocks.watched.push(taskId);
    await runStorageJob(taskId, handlers);
    return { config: recoveredConfig, restart_required: false };
  },
}));

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

const recoveredConfig: DockerDaemonConfig = {
  ...config,
  data_root: "/mnt/pool/docker",
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
    mocks.failWith = null;
    mocks.hold = null;
    mocks.progress = null;
    mocks.tasks = [];
    mocks.watched = [];
    mocks.toast.error.mockClear();
    mocks.toast.success.mockClear();
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
      await screen.findByText("Stop all containers to change Docker storage?"),
    ).toBeInTheDocument();
    expect(screen.getByText(/reverse proxy/)).toBeInTheDocument();
    expect(mocks.applyStorage).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Change storage" }));

    await waitFor(() =>
      expect(mocks.applyStorage).toHaveBeenCalledWith({
        ...config,
        data_root: "/mnt/pool/docker",
        image_store: "classic",
        storage_driver: "btrfs",
      }),
    );
    expect(mocks.save).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(
        screen.queryByText("Stop all containers to change Docker storage?"),
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

  it("accepts an IPv6 address pool the daemon accepts", async () => {
    const { user } = renderSection();

    await user.type(screen.getByLabelText("Base"), "fd00::/48");
    await user.type(screen.getByLabelText("Network size"), "64");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith({
        ...config,
        address_pool_base: "fd00::/48",
        address_pool_size: 64,
      }),
    );
    expect(screen.queryByText(/Use a CIDR/)).not.toBeInTheDocument();
  });

  it("bounds an IPv6 address pool size by 128 and rejects a bad base", async () => {
    const { user } = renderSection();

    await user.type(screen.getByLabelText("Base"), "fd00::/48");
    await user.type(screen.getByLabelText("Network size"), "129");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText("Use a prefix between 48 and 128."),
    ).toBeInTheDocument();

    await user.clear(screen.getByLabelText("Base"));
    await user.type(screen.getByLabelText("Base"), "999.1.1.1/16");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText("Use a CIDR like 172.20.0.0/16 or fd00::/48."),
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

  describe("while a storage change runs", () => {
    const TITLE = "Stop all containers to change Docker storage?";

    const holdJob = () => {
      let finish: () => void = () => {};
      mocks.hold = new Promise<void>((resolve) => {
        finish = resolve;
      });
      return finish;
    };

    const startStorageChange = async () => {
      const view = renderSection();
      await view.user.type(
        screen.getByLabelText("Data root"),
        "/mnt/pool/docker",
      );
      await view.user.click(screen.getByRole("button", { name: "Save" }));
      await screen.findByText(TITLE);
      await view.user.click(
        screen.getByRole("button", { name: "Change storage" }),
      );
      await waitFor(() => expect(mocks.applyStorage).toHaveBeenCalledTimes(1));
      return view;
    };

    it("closes the confirm, keeps the job going and holds the other controls", async () => {
      const finish = holdJob();
      mocks.progress = "Stopping containers";
      const { user } = await startStorageChange();

      expect(await screen.findByText("Stopping containers")).toBeVisible();
      expect(
        screen.getByRole("button", { name: "Changing storage…" }),
      ).toBeDisabled();
      await user.click(screen.getByRole("button", { name: "Close" }));
      await waitFor(() =>
        expect(screen.queryByText(TITLE)).not.toBeInTheDocument(),
      );

      // The section shows the step, and nothing can start a second change.
      expect(screen.getByText("Changing Docker storage")).toBeVisible();
      expect(screen.getByText("Stopping containers")).toBeVisible();
      expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
      expect(screen.getByLabelText("Data root")).toBeDisabled();
      expect(screen.getByLabelText("Storage driver")).toHaveAttribute(
        "aria-disabled",
        "true",
      );
      expect(mocks.applyStorage).toHaveBeenCalledTimes(1);
      expect(mocks.toast.success).not.toHaveBeenCalled();

      finish();
      await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledTimes(1));
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Docker restarted on the new storage. Deploy your stacks again.",
      );
      await waitFor(() =>
        expect(
          screen.queryByText("Changing Docker storage"),
        ).not.toBeInTheDocument(),
      );
      expect(screen.queryByText(TITLE)).not.toBeInTheDocument();
      expect(mocks.applyStorage).toHaveBeenCalledTimes(1);
    });

    it("closes with Escape and reports a failure once", async () => {
      const finish = holdJob();
      mocks.failWith = new Error("docker did not start");
      const { user } = await startStorageChange();

      await user.keyboard("{Escape}");
      await waitFor(() =>
        expect(screen.queryByText(TITLE)).not.toBeInTheDocument(),
      );
      expect(mocks.toast.error).not.toHaveBeenCalled();

      finish();
      await waitFor(() => expect(mocks.toast.error).toHaveBeenCalledTimes(1));
      expect(mocks.toast.success).not.toHaveBeenCalled();
      expect(screen.queryByText(TITLE)).not.toBeInTheDocument();
    });

    it("takes up a change that is already running when the section opens", async () => {
      const finish = holdJob();
      mocks.progress = "Restarting Docker";
      mocks.tasks = [
        {
          id: "task-9",
          metadata: { label: "Docker storage change" },
          state: "running",
          type: "docker.apply_daemon_storage",
        },
      ];
      renderSection();

      expect(await screen.findByText("Restarting Docker")).toBeVisible();
      expect(screen.getByText("Changing Docker storage")).toBeVisible();
      expect(mocks.watched).toEqual(["task-9"]);
      expect(mocks.applyStorage).not.toHaveBeenCalled();
      expect(screen.getByLabelText("Data root")).toBeDisabled();

      finish();
      await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledTimes(1));
      await waitFor(() =>
        expect(
          screen.queryByText("Changing Docker storage"),
        ).not.toBeInTheDocument(),
      );
      expect(screen.getByLabelText("Data root")).toBeEnabled();
      expect(screen.getByLabelText("Data root")).toHaveValue(
        "/mnt/pool/docker",
      );
    });

    it("leaves alone a job that has ended or another type", async () => {
      mocks.tasks = [
        {
          id: "task-1",
          state: "completed",
          type: "docker.apply_daemon_storage",
        },
        { id: "task-2", state: "running", type: "docker.compose" },
      ];
      renderSection();

      await waitFor(() =>
        expect(screen.getByLabelText("Data root")).toBeEnabled(),
      );
      expect(mocks.watched).toEqual([]);
      expect(
        screen.queryByText("Changing Docker storage"),
      ).not.toBeInTheDocument();
    });

    it("does not take up a job without a privileged session", async () => {
      mocks.tasks = [
        { id: "task-9", state: "running", type: "docker.apply_daemon_storage" },
      ];
      renderSection(false);

      expect(screen.getByLabelText("Data root")).toBeDisabled();
      expect(mocks.watched).toEqual([]);
    });
  });
});
