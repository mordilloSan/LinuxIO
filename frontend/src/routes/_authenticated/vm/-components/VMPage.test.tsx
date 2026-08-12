import { beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError, linuxio } from "@/api";
import {
  act,
  createTestQueryClient,
  renderWithTanStackRouter,
  screen,
  waitFor,
  within,
} from "@/test/render";

import VMMachinesLayout from "./VMMachinesLayout";
import VMPage from "./VMPage";

const mocks = vi.hoisted(() => {
  const managedISOPath = "/var/lib/libvirt/images/linuxio/isos";
  const readyPreflight = {
    defaultNetworkActive: true,
    defaultNetworkExists: true,
    defaultPoolActive: true,
    defaultPoolExists: true,
    errors: [] as string[],
    firmware: { biosAvailable: true, uefiAvailable: true },
    isoReadable: true,
    kvmPresent: true,
    libvirtReachable: true,
    managedPaths: {
      cloudImages: "/var/lib/libvirt/images/linuxio/cloud-images",
      isos: managedISOPath,
      root: "/var/lib/libvirt/images/linuxio",
    },
    qemuPresent: true,
    warnings: [] as string[],
  };

  const alpha = {
    autostart: false,
    diskGB: 16,
    disks: [
      {
        device: "disk",
        owned: true,
        path: "/var/lib/libvirt/images/linuxio-alpha.qcow2",
        sizeGB: 16,
        target: "vda",
        volumeName: "linuxio-alpha.qcow2",
      },
    ],
    hasGraphics: true,
    memoryMB: 2048,
    name: "alpha",
    nics: [
      {
        ipAddresses: ["192.168.122.57"],
        mac: "52:54:00:00:00:01",
        model: "virtio",
        network: "default",
      },
    ],
    ownedDisks: ["/var/lib/libvirt/images/linuxio-alpha.qcow2"],
    state: "running",
    uuid: "vm-alpha",
    vcpus: 2,
  };
  const beta = { ...alpha, name: "beta", state: "shut off", uuid: "vm-beta" };

  return {
    alpha,
    beta,
    managedISOPath,
    listVMs: [alpha],
    mutations: {
      forceOff: vi.fn(),
      reboot: vi.fn(),
      resume: vi.fn(),
      shutdown: vi.fn(),
      start: vi.fn(),
      suspend: vi.fn(),
    },
    openVMConsoleStream: vi.fn(),
    readyPreflight,
    preflight: readyPreflight,
    routeNavigate: vi.fn(),
    // Detail selection is a path param on /vm/machines/$name.
    routeParams: { name: "alpha" },
    resourceGet: vi.fn(),
    resourcePost: vi.fn(),
    resourceStat: vi.fn(),
    refetchList: vi.fn(),
    rfbConstructor: vi.fn(),
    rfbDisconnect: vi.fn(),
    openTaskWatchStream: vi.fn(),
    virtCreate: vi.fn(),
    virtDelete: vi.fn(),
    waitForStreamResult: vi.fn(),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    getRouteApi: () => ({
      useNavigate: () => mocks.routeNavigate,
    }),
    useParams: () => mocks.routeParams,
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { useState } = await import("react");
  // Mirrors useCallMutation: on success runs invalidates -> success ->
  // options.onSuccess, with the unwrapped task result (undefined here).
  const taskAction = (
    fn: (request: unknown) => unknown,
    config?: {
      invalidates?:
        | unknown[]
        | ((result: unknown, variables: unknown) => unknown[]);
      success?: string | ((result: unknown, variables: unknown) => void);
      error?: string | ((error: unknown, variables: unknown) => void);
      options?: { onSuccess?: (result: unknown, variables: unknown) => void };
    },
    result?: unknown,
  ) => ({
    isPending: false,
    mutate: (request: unknown) => {
      fn(request);
      if (typeof config?.invalidates === "function") {
        config.invalidates(result, request);
      }
      if (typeof config?.success === "function") {
        config.success(result, request);
      }
      config?.options?.onSuccess?.(result, request);
    },
    mutateAsync: fn,
  });
  type TaskActionConfig = Parameters<typeof taskAction>[1];
  // Mirrors useTaskStreamAction: submit -> onTaskStart -> attach -> progress
  // frames -> result, driven by the openTaskWatchStream/waitForStreamResult
  // mocks so tests control the task stream.
  interface TaskStreamActionConfig extends NonNullable<TaskActionConfig> {
    onTaskStart?: (task: unknown, variables: unknown) => void;
    onOpen?: (stream: unknown, task: unknown, variables: unknown) => void;
    onProgress?: (progress: unknown, task: unknown, variables: unknown) => void;
    options?: {
      onMutate?: (variables: unknown) => void;
      onSettled?: () => void;
      onSuccess?: (result: unknown, variables: unknown) => void;
    };
  }
  // Called during component render, so it can hold pending state in a hook.
  const useTaskStreamActionMock = (
    submit: (request: unknown) => Promise<{ id: string }>,
    config?: TaskStreamActionConfig,
  ) => {
    const [isPending, setIsPending] = useState(false);
    const run = async (request: unknown) => {
      setIsPending(true);
      config?.options?.onMutate?.(request);
      try {
        const task = await submit(request);
        config?.onTaskStart?.(task, request);
        const stream = mocks.openTaskWatchStream(task.id);
        config?.onOpen?.(stream, task, request);
        const result = await mocks.waitForStreamResult(stream, {
          onProgress: (progress: unknown) =>
            config?.onProgress?.(progress, task, request),
        });
        if (typeof config?.invalidates === "function") {
          config.invalidates(result, request);
        }
        if (typeof config?.success === "function") {
          config.success(result, request);
        }
        config?.options?.onSuccess?.(result, request);
        return result;
      } catch (error) {
        if (typeof config?.error === "function") {
          config.error(error, request);
        }
        throw error;
      } finally {
        setIsPending(false);
        config?.options?.onSettled?.();
      }
    };
    return {
      isPending,
      mutate: (request: unknown) => {
        void run(request).catch(() => undefined);
      },
      mutateAsync: run,
    };
  };
  const useDirectActionMock = (
    runAction: (request: unknown) => Promise<unknown>,
    config?: TaskActionConfig,
  ) => {
    const [isPending, setIsPending] = useState(false);
    const run = async (request: unknown) => {
      setIsPending(true);
      try {
        const result = await runAction(request);
        if (typeof config?.invalidates === "function") {
          config.invalidates(result, request);
        }
        if (typeof config?.success === "function") {
          config.success(result, request);
        }
        config?.options?.onSuccess?.(result, request);
        return result;
      } catch (error) {
        if (typeof config?.error === "function") {
          config.error(error, request);
        }
        throw error;
      } finally {
        setIsPending(false);
      }
    };
    return {
      isPending,
      mutate: (request: unknown) => {
        void run(request).catch(() => undefined);
      },
      mutateAsync: run,
    };
  };
  const useCallMutationMock = (
    endpoint: { route?: string },
    config?: TaskActionConfig,
  ) => {
    const handlers: Record<string, (request: unknown) => Promise<unknown>> = {
      "filebrowser.resource_post": mocks.resourcePost,
      "virt.delete": mocks.virtDelete,
      "virt.force_off": mocks.mutations.forceOff,
      "virt.reboot": mocks.mutations.reboot,
      "virt.resume": mocks.mutations.resume,
      "virt.shutdown": mocks.mutations.shutdown,
      "virt.start": mocks.mutations.start,
      "virt.suspend": mocks.mutations.suspend,
    };
    return useDirectActionMock(
      handlers[endpoint.route ?? ""] ?? vi.fn().mockResolvedValue(undefined),
      config,
    );
  };
  const callDescriptor = (
    route: string,
    queryKey: unknown[],
    queryFn: () => unknown,
  ) => ({
    queryFn,
    queryKey,
    route,
  });
  const requestCall = <TRequest extends object>(
    route: string,
    queryKey: (request: TRequest) => unknown[],
    queryFn: (request: TRequest) => unknown,
  ) =>
    Object.assign(
      (request: TRequest) => ({
        queryFn: () => queryFn(request),
        queryKey: queryKey(request),
        route,
      }),
      { route },
    );

  const resourceGet = requestCall(
    "filebrowser.resource_get",
    (request: { path: string }) => [
      "linuxio",
      "filebrowser",
      "resource_get",
      request,
    ],
    (request: { path: string }) => mocks.resourceGet(request),
  );
  const resourcePost = Object.assign(mocks.resourcePost, {
    route: "filebrowser.resource_post",
  });
  const resourceStat = Object.assign(
    requestCall(
      "filebrowser.resource_stat",
      (request: { path: string }) => [
        "linuxio",
        "filebrowser",
        "resource_stat",
        request,
      ],
      (request: { path: string }) => mocks.resourceStat(request),
    ),
  );

  return {
    ...actual,
    useCallMutation: useCallMutationMock,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        resource_get: resourceGet,
        resource_post: resourcePost,
        resource_stat: resourceStat,
      },
      virt: {
        create: Object.assign(mocks.virtCreate, {
          useTaskStreamAction: (config?: TaskStreamActionConfig) =>
            useTaskStreamActionMock(mocks.virtCreate, config),
        }),
        delete: Object.assign(mocks.virtDelete, { route: "virt.delete" }),
        force_off: Object.assign(mocks.mutations.forceOff, {
          route: "virt.force_off",
        }),
        get: requestCall(
          "virt.get",
          (request: { name: string }) => ["linuxio", "virt", "get", request],
          (request: { name: string }) =>
            Promise.resolve(
              mocks.listVMs.find((vm) => vm.name === request.name),
            ),
        ),
        list: callDescriptor("virt.list", ["linuxio", "virt", "list"], () =>
          Promise.resolve(mocks.listVMs),
        ),
        preflight: requestCall(
          "virt.preflight",
          () => ["linuxio", "virt", "preflight"],
          () => Promise.resolve(mocks.preflight),
        ),
        reboot: Object.assign(mocks.mutations.reboot, {
          route: "virt.reboot",
        }),
        resume: Object.assign(mocks.mutations.resume, {
          route: "virt.resume",
        }),
        shutdown: Object.assign(mocks.mutations.shutdown, {
          route: "virt.shutdown",
        }),
        start: Object.assign(mocks.mutations.start, { route: "virt.start" }),
        suspend: Object.assign(mocks.mutations.suspend, {
          route: "virt.suspend",
        }),
      },
    },
    call: (route: string, request?: unknown) => {
      if (route === "filebrowser.resource_stat") {
        return mocks.resourceStat(request);
      }
      return Promise.resolve(undefined);
    },
    openTaskWatchStream: mocks.openTaskWatchStream,
    openVMConsoleStream: mocks.openVMConsoleStream,
    waitForStreamResult: mocks.waitForStreamResult,
  };
});

vi.mock("@novnc/novnc", () => {
  const RFBMock = vi.fn(function (
    this: Record<string, unknown>,
    target: unknown,
    channel: unknown,
    options: unknown,
  ) {
    mocks.rfbConstructor(target, channel, options);
    this.addEventListener = vi.fn();
    this.disconnect = mocks.rfbDisconnect;
    this.focusOnClick = false;
    this.removeEventListener = vi.fn();
    this.resizeSession = false;
    this.scaleViewport = false;
  });
  return { default: RFBMock };
});

function fakeConsoleStream() {
  return {
    abort: vi.fn(),
    close: vi.fn(),
    id: 1,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: vi.fn(),
    status: "open",
    type: "virt.console_open",
    write: vi.fn(),
  };
}

function fakeTaskStream() {
  return {
    abort: vi.fn(),
    close: vi.fn(),
    id: 2,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: vi.fn(),
    status: "open",
    type: "tasks.watch",
    write: vi.fn(),
  };
}

function fakeTaskSnapshot(id: string, type: string) {
  return {
    created_at: "2026-06-23T00:00:00Z",
    id,
    state: "running",
    type,
    updated_at: "2026-06-23T00:00:00Z",
  };
}

async function renderVMPage(
  libvirtAvailable = true,
  queryClient = createTestQueryClient(),
) {
  queryClient.setQueryData(linuxio.virt.list.queryKey, mocks.listVMs);
  queryClient.setQueryData(
    linuxio.virt.preflight({}).queryKey,
    mocks.preflight,
  );
  const result = renderWithTanStackRouter(
    <VMPage>
      <VMMachinesLayout />
    </VMPage>,
    {
      auth: {
        isAuthenticated: true,
        libvirtAvailable,
        privileged: true,
      },
      queryClient,
    },
  );
  if (libvirtAvailable) {
    await screen.findByRole("tab", { name: /virtual machines/i });
  } else {
    await screen.findByText(/libvirt unavailable/i);
  }
  return { ...result, queryClient };
}

beforeEach(() => {
  mocks.listVMs = [mocks.alpha];
  mocks.openTaskWatchStream.mockReset();
  mocks.openTaskWatchStream.mockReturnValue(fakeTaskStream());
  mocks.openVMConsoleStream.mockReset();
  mocks.openVMConsoleStream.mockReturnValue(fakeConsoleStream());
  mocks.virtCreate.mockReset();
  mocks.virtCreate.mockResolvedValue(
    fakeTaskSnapshot("task-create", "virt.create"),
  );
  mocks.virtDelete.mockReset();
  mocks.virtDelete.mockResolvedValue({ failed: [], removed: [] });
  mocks.preflight = {
    ...mocks.readyPreflight,
    firmware: { ...mocks.readyPreflight.firmware },
    warnings: [],
  };
  mocks.routeNavigate.mockReset();
  mocks.routeParams = { name: "alpha" };
  mocks.resourceGet.mockReset();
  mocks.resourceGet.mockImplementation(({ path }: { path: string }) => {
    if (path === "/") {
      return Promise.resolve({
        folders: [{ name: "isos", type: "directory" }],
        name: "/",
        path: "/",
        type: "directory",
      });
    }
    if (path === "/isos/") {
      return Promise.resolve({
        files: [
          { name: "debian.iso", type: "file" },
          { name: "notes.txt", type: "file" },
        ],
        folders: [],
        name: "isos",
        path: "/isos/",
        type: "directory",
      });
    }
    if (path === `${mocks.managedISOPath}/`) {
      return Promise.resolve({
        files: [
          { name: "debian.iso", type: "file" },
          { name: "notes.txt", type: "file" },
        ],
        folders: [],
        name: "isos",
        path: `${mocks.managedISOPath}/`,
        type: "directory",
      });
    }
    return Promise.resolve({
      folders: [],
      name: path,
      path,
      type: "directory",
    });
  });
  mocks.resourcePost.mockReset();
  mocks.resourcePost.mockResolvedValue(undefined);
  mocks.resourceStat.mockReset();
  mocks.resourceStat.mockResolvedValue({
    group: "miguelmariz",
    mode: "drwxrwxr-x",
    modified: "2026-06-20T00:00:00Z",
    name: "isos",
    owner: "miguelmariz",
    path: "/isos",
    permissions: "775",
    raw: "",
    realPath: "/isos",
    size: 0,
  });
  mocks.refetchList.mockReset();
  mocks.rfbConstructor.mockReset();
  mocks.rfbDisconnect.mockReset();
  mocks.waitForStreamResult.mockReset();
  mocks.waitForStreamResult.mockImplementation(async (_stream, options) => {
    options?.onProgress?.({
      percentage: 100,
      phase: "complete",
      message: "VM created",
      detail: {
        message: "VM created",
        percent: 100,
        phase: "complete",
      },
    });
    return { ...mocks.alpha, name: "created" };
  });
  Object.values(mocks.mutations).forEach((fn) => fn.mockReset());
});

describe("Virtual Machines page", () => {
  it("shows the libvirt capability warning when unavailable", async () => {
    await renderVMPage(false);

    expect(screen.getByText(/libvirt unavailable/i)).toBeInTheDocument();
    expect(
      screen.getByText(/libvirt daemon is unavailable/i),
    ).toBeInTheDocument();
  });

  it("renders the VM section tabs", async () => {
    await renderVMPage();

    expect(
      screen.getByRole("tab", { name: /global dashboard/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /networks/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /images/i })).toBeInTheDocument();
    expect(
      screen.getByRole("tab", { name: /virtual machines/i }),
    ).toBeInTheDocument();
  });

  it("renders VM rows and dispatches lifecycle actions", async () => {
    const { user } = await renderVMPage();

    expect(screen.getByRole("button", { name: "alpha" })).toBeInTheDocument();
    expect(screen.getAllByText("192.168.122.57").length).toBeGreaterThanOrEqual(
      1,
    );
    expect(screen.getAllByText("running").length).toBeGreaterThanOrEqual(1);

    await user.click(screen.getByRole("button", { name: "Shutdown" }));

    expect(mocks.mutations.shutdown).toHaveBeenCalledWith({ name: "alpha" });
  });

  it("keeps lifecycle feedback scoped to each VM until its action settles", async () => {
    let resolveShutdown!: () => void;
    let resolveStart!: () => void;
    mocks.listVMs = [mocks.alpha, mocks.beta];
    mocks.mutations.shutdown.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveShutdown = resolve;
        }),
    );
    mocks.mutations.start.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveStart = resolve;
        }),
    );
    const { user } = await renderVMPage();
    const alphaRow = screen
      .getByRole("button", { name: "alpha" })
      .closest<HTMLElement>('[role="row"]');
    const betaRow = screen
      .getByRole("button", { name: "beta" })
      .closest<HTMLElement>('[role="row"]');

    expect(alphaRow).not.toBeNull();
    expect(betaRow).not.toBeNull();

    await user.click(
      within(alphaRow!).getByRole("button", { name: "Shutdown" }),
    );

    expect(
      within(
        within(alphaRow!).getByRole("button", { name: "Shutdown" }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();
    expect(
      within(alphaRow!).getByRole("button", { name: "Reboot" }),
    ).toBeDisabled();
    expect(
      within(betaRow!).getByRole("button", { name: "Start" }),
    ).toBeEnabled();

    await user.click(within(betaRow!).getByRole("button", { name: "Start" }));

    expect(
      within(within(betaRow!).getByRole("button", { name: "Start" })).getByRole(
        "progressbar",
      ),
    ).toBeInTheDocument();

    await act(async () => {
      resolveShutdown();
    });

    await waitFor(() => {
      expect(
        within(alphaRow!).queryByRole("progressbar"),
      ).not.toBeInTheDocument();
    });
    expect(within(betaRow!).getByRole("progressbar")).toBeInTheDocument();

    await act(async () => {
      resolveStart();
    });

    await waitFor(() => {
      expect(
        within(betaRow!).queryByRole("progressbar"),
      ).not.toBeInTheDocument();
    });
  });

  it("does not crash when delete success has no disk arrays", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = screen.getByRole("dialog");

    await user.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(mocks.virtDelete).toHaveBeenCalledWith({
        deleteDisks: true,
        name: "alpha",
      });
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("deletes and evicts only the clicked VM while URL selection points elsewhere", async () => {
    const queryClient = createTestQueryClient();
    const removeQueries = vi.spyOn(queryClient, "removeQueries");
    mocks.listVMs = [mocks.alpha, mocks.beta];
    mocks.routeParams = { name: "alpha" };
    const { user } = await renderVMPage(true, queryClient);

    await user.click(screen.getAllByRole("button", { name: "Delete" })[1]);
    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText(/delete beta from libvirt/i),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(mocks.virtDelete).toHaveBeenCalledWith({
        deleteDisks: true,
        name: "beta",
      }),
    );
    await waitFor(() => expect(removeQueries).toHaveBeenCalledOnce());
    expect(removeQueries).toHaveBeenCalledWith({
      queryKey: ["linuxio", "virt", "get", { name: "beta" }],
    });
  });

  it("keeps the delete dialog synced with the live VM list", async () => {
    const { queryClient, user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(
      within(screen.getByRole("dialog")).getByText(
        "/var/lib/libvirt/images/linuxio-alpha.qcow2",
      ),
    ).toBeInTheDocument();

    const refreshedAlpha = {
      ...mocks.alpha,
      ownedDisks: ["/var/lib/libvirt/images/refreshed-alpha.qcow2"],
      state: "shut off",
    };
    mocks.listVMs = [refreshedAlpha];
    act(() => {
      queryClient.setQueryData(linuxio.virt.list.queryKey, [refreshedAlpha]);
    });

    expect(
      await within(screen.getByRole("dialog")).findByText(
        "/var/lib/libvirt/images/refreshed-alpha.qcow2",
      ),
    ).toBeInTheDocument();

    mocks.listVMs = [];
    act(() => {
      queryClient.setQueryData(linuxio.virt.list.queryKey, []);
    });

    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("keeps the delete dialog open while the delete action is pending", async () => {
    let resolveDelete!: (result: unknown) => void;
    mocks.virtDelete.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveDelete = resolve;
        }),
    );
    const { queryClient, user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = screen.getByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(mocks.virtDelete).toHaveBeenCalledWith({
        deleteDisks: true,
        name: "alpha",
      }),
    );

    // The backend undefines the domain before disk cleanup finishes, so the
    // polled list drops the row while the delete action is still pending.
    mocks.listVMs = [];
    act(() => {
      queryClient.setQueryData(linuxio.virt.list.queryKey, []);
    });

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(
      within(screen.getByRole("dialog")).getByText(
        /delete alpha from libvirt/i,
      ),
    ).toBeInTheDocument();

    await act(async () => {
      resolveDelete({ failed: [], removed: [] });
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("does not show delete success when the delete action fails", async () => {
    mocks.virtDelete.mockRejectedValueOnce(
      new Error("Domain not found: no domain with matching name 'alpha'"),
    );
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = screen.getByRole("dialog");

    await user.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(mocks.virtDelete).toHaveBeenCalledWith({
        deleteDisks: true,
        name: "alpha",
      });
    });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("allows create when the default storage pool can be repaired", async () => {
    mocks.preflight = {
      ...mocks.readyPreflight,
      defaultPoolActive: false,
      defaultPoolExists: false,
      warnings: [
        "default storage pool is missing; create will define it at /var/lib/libvirt/images",
      ],
    };

    await renderVMPage();

    expect(
      screen.getByText(/default storage pool is missing/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /create vm/i })).toBeEnabled();
  });

  it("submits the create dialog request shape", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.type(within(dialog).getByLabelText(/name/i), "beta");
    await user.clear(within(dialog).getByLabelText(/vcpus/i));
    await user.type(within(dialog).getByLabelText(/vcpus/i), "4");
    await user.clear(within(dialog).getByLabelText(/memory mb/i));
    await user.type(within(dialog).getByLabelText(/memory mb/i), "8192");
    await user.clear(within(dialog).getByLabelText(/disk gb/i));
    await user.type(within(dialog).getByLabelText(/disk gb/i), "64");
    await user.type(
      within(dialog).getByLabelText(/iso path/i),
      "/isos/beta.iso",
    );
    await user.click(within(dialog).getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(mocks.virtCreate).toHaveBeenCalledWith({
        diskGB: 64,
        isoPath: "/isos/beta.iso",
        memoryMB: 8192,
        name: "beta",
        network: "default",
        sourceType: "iso",
        start: true,
        vcpus: 4,
      });
    });
  });

  it("does not submit a directory path as install media", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.type(within(dialog).getByLabelText(/name/i), "badmedia");
    await user.type(
      within(dialog).getByLabelText(/iso path/i),
      "/home/miguelmariz",
    );

    expect(
      within(dialog).getByText(/folders cannot be used as install media/i),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Create" }),
    ).toBeDisabled();
    expect(mocks.virtCreate).not.toHaveBeenCalled();
  });

  it("does not show presets for custom ISO installers", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    expect(
      within(dialog).queryByRole("radiogroup", { name: /vm preset/i }),
    ).not.toBeInTheDocument();
    expect(
      within(dialog).queryByRole("radio", { name: /linux desktop/i }),
    ).not.toBeInTheDocument();
  });

  it("creates a Home Assistant OS VM from the image preset", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.click(within(dialog).getByRole("tab", { name: /ready image/i }));
    await user.click(
      within(dialog).getByRole("radio", { name: /home assistant os/i }),
    );
    expect(
      within(dialog).queryByLabelText(/iso path/i),
    ).not.toBeInTheDocument();

    await user.type(within(dialog).getByLabelText(/name/i), "homeassistant");
    await user.click(within(dialog).getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(mocks.virtCreate).toHaveBeenCalledWith({
        diskGB: 32,
        imagePresetId: "home-assistant-os",
        memoryMB: 4096,
        name: "homeassistant",
        network: "default",
        sourceType: "imagePreset",
        start: true,
        vcpus: 2,
      });
    });
  });

  it("creates a Debian Server VM from a ready cloud image", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.click(within(dialog).getByRole("tab", { name: /ready image/i }));
    await user.click(
      within(dialog).getByRole("radio", { name: /debian server/i }),
    );
    expect(
      within(dialog).queryByLabelText(/iso path/i),
    ).not.toBeInTheDocument();

    await user.type(within(dialog).getByLabelText(/^name/i), "debian");
    await user.type(
      within(dialog).getByLabelText(/login password/i),
      "secret-password",
    );
    await user.click(within(dialog).getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(mocks.virtCreate).toHaveBeenCalledWith({
        cloudInitPassword: "secret-password",
        cloudInitUsername: "linuxio",
        diskGB: 20,
        imagePresetId: "debian-server",
        memoryMB: 2048,
        name: "debian",
        network: "default",
        sourceType: "imagePreset",
        start: true,
        vcpus: 2,
      });
    });
  });

  it("shows VM create progress while a ready image imports", async () => {
    let resolveCreate: (value: unknown) => void = () => {};
    mocks.waitForStreamResult.mockImplementationOnce(
      async (_stream, options) => {
        options?.onProgress?.({
          percentage: 10,
          phase: "download",
          message: "Downloading Debian Server image (10.0 MiB / 100.0 MiB)",
          detail: {
            message: "Downloading Debian Server image (10.0 MiB / 100.0 MiB)",
            path: "/var/lib/libvirt/images/linuxio/cloud-images/linuxio-debian.qcow2.download",
            percent: 10,
            phase: "download",
          },
        });
        return await new Promise((resolve) => {
          resolveCreate = resolve;
        });
      },
    );
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.click(within(dialog).getByRole("tab", { name: /ready image/i }));
    await user.click(
      within(dialog).getByRole("radio", { name: /debian server/i }),
    );
    await user.type(within(dialog).getByLabelText(/^name/i), "debian");
    await user.type(
      within(dialog).getByLabelText(/login password/i),
      "secret-password",
    );
    await user.click(within(dialog).getByRole("button", { name: "Create" }));

    expect(
      await screen.findByText(/Downloading Debian Server image/i),
    ).toBeInTheDocument();
    expect(
      screen
        .getAllByRole("progressbar")
        .some((bar) => bar.getAttribute("aria-valuenow") === "10"),
    ).toBe(true);

    resolveCreate({ ...mocks.alpha, name: "debian" });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("selects an ISO path from the file tree picker", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.click(
      within(dialog).getByRole("button", { name: /browse iso files/i }),
    );
    await user.click(await screen.findByText(`${mocks.managedISOPath}/`));
    await user.click(await screen.findByText("debian.iso"));

    expect(mocks.resourceGet).toHaveBeenCalledWith({
      path: `${mocks.managedISOPath}/`,
    });
    expect(screen.queryByText("notes.txt")).not.toBeInTheDocument();
    expect(within(dialog).getByLabelText(/iso path/i)).toHaveValue(
      `${mocks.managedISOPath}/debian.iso`,
    );
  });

  it("keeps the browsed folder in the ISO path when the tree closes", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.click(
      within(dialog).getByRole("button", { name: /browse iso files/i }),
    );
    await user.click(await screen.findByText(`${mocks.managedISOPath}/`));
    await screen.findByText("debian.iso");

    const isoInput = within(dialog).getByLabelText(/iso path/i);
    expect(isoInput).toHaveValue(`${mocks.managedISOPath}/`);

    await user.keyboard("{Escape}");

    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
    expect(isoInput).toHaveValue(`${mocks.managedISOPath}/`);
    expect(
      within(dialog).getByRole("button", { name: "Create" }),
    ).toBeDisabled();
  });

  it("creates a missing folder derived from typed ISO path text", async () => {
    mocks.resourceStat.mockRejectedValueOnce(
      new LinuxIOError("path wording is presentation only", 404),
    );
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.type(
      within(dialog).getByLabelText(/iso path/i),
      "/missing/debian.iso",
    );
    await user.tab();

    await waitFor(() => {
      expect(mocks.resourceStat).toHaveBeenCalledWith({ path: "/missing" });
      expect(mocks.resourcePost).toHaveBeenCalledWith({ path: "/missing/" });
    });
  });

  it("opens the noVNC console over a mux stream", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: "Console" }));

    expect(mocks.openVMConsoleStream).toHaveBeenCalledWith("alpha");
    await waitFor(() => expect(mocks.rfbConstructor).toHaveBeenCalled());
  });

  it("shows console stream result errors", async () => {
    const { user } = await renderVMPage();

    await user.click(screen.getByRole("button", { name: "Console" }));
    const stream = mocks.openVMConsoleStream.mock.results[0]?.value;

    stream.onResult?.({
      code: 500,
      error: 'wait for VNC socket: VM "alpha" has no VNC unix socket',
      status: "error",
    });

    expect(await screen.findByText(/has no VNC unix socket/i)).toBeVisible();
    expect(screen.getByText("Unavailable")).toBeVisible();
  });
});
