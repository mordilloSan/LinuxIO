import { beforeEach, describe, expect, it, vi } from "vitest";

import Page from "./index";

import { render, screen, waitFor, within } from "@/test/render";

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

  return {
    alpha,
    managedISOPath,
    listVMs: [alpha],
    mutations: {
      create: vi.fn(),
      delete: vi.fn(),
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
    resourceGet: vi.fn(),
    resourcePost: vi.fn(),
    resourceStat: vi.fn(),
    refetchList: vi.fn(),
    rfbConstructor: vi.fn(),
    rfbDisconnect: vi.fn(),
    openJobAttachStream: vi.fn(),
    virtCreate: vi.fn(),
    virtDelete: vi.fn(),
    waitForStreamResult: vi.fn(),
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const mutation = (
    fn: (request: unknown) => unknown,
    options?: { onSuccess?: (result: unknown) => void },
    successResult?: unknown,
  ) => ({
    isPending: false,
    mutate: (request: unknown) => {
      fn(request);
      if (successResult !== undefined) {
        options?.onSuccess?.(successResult);
      }
    },
    mutateAsync: fn,
  });
  const resourceGet = Object.assign(mocks.resourceGet, {
    queryKey: (request: { path: string }) => [
      "linuxio",
      "filebrowser",
      "resource_get",
      request,
    ],
  });
  const resourcePost = Object.assign(mocks.resourcePost, {
    useMutation: () => mutation(mocks.resourcePost),
  });

  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        resource_get: resourceGet,
        resource_post: resourcePost,
        resource_stat: mocks.resourceStat,
      },
      virt: {
        create: Object.assign(mocks.virtCreate, {
          useMutation: () => mutation(mocks.mutations.create),
        }),
        delete: Object.assign(mocks.virtDelete, {
          useMutation: (options?: { onSuccess?: (result: unknown) => void }) =>
            mutation(mocks.mutations.delete, options, {}),
        }),
        force_off: {
          useMutation: () => mutation(mocks.mutations.forceOff),
        },
        get: {
          queryKey: (name: string) => ["linuxio", "virt", "get", { name }],
          useQuery: (name: string) => ({
            data: mocks.listVMs.find((vm) => vm.name === name),
          }),
        },
        list: {
          queryKey: () => ["linuxio", "virt", "list"],
          useQuery: () => ({
            data: mocks.listVMs,
            isLoading: false,
            refetch: mocks.refetchList,
          }),
        },
        preflight: {
          useQuery: () => ({ data: mocks.preflight, refetch: vi.fn() }),
        },
        reboot: {
          useMutation: () => mutation(mocks.mutations.reboot),
        },
        resume: {
          useMutation: () => mutation(mocks.mutations.resume),
        },
        shutdown: {
          useMutation: () => mutation(mocks.mutations.shutdown),
        },
        start: {
          useMutation: () => mutation(mocks.mutations.start),
        },
        suspend: {
          useMutation: () => mutation(mocks.mutations.suspend),
        },
      },
    },
    openJobAttachStream: mocks.openJobAttachStream,
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

function fakeJobStream() {
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
    type: "jobs.attach",
    write: vi.fn(),
  };
}

function fakeJobSnapshot(id: string, type: string) {
  return {
    created_at: "2026-06-23T00:00:00Z",
    id,
    state: "running",
    type,
    updated_at: "2026-06-23T00:00:00Z",
  };
}

function renderVMPage(libvirtAvailable = true) {
  return render(<Page />, {
    auth: {
      isAuthenticated: true,
      libvirtAvailable,
      privileged: true,
    },
  });
}

async function openVirtualMachinesTab(user: {
  click: (target: Element) => unknown;
}) {
  await user.click(screen.getByRole("tab", { name: /virtual machines/i }));
}

beforeEach(() => {
  mocks.listVMs = [mocks.alpha];
  mocks.openJobAttachStream.mockReset();
  mocks.openJobAttachStream.mockReturnValue(fakeJobStream());
  mocks.openVMConsoleStream.mockReset();
  mocks.openVMConsoleStream.mockReturnValue(fakeConsoleStream());
  mocks.virtCreate.mockReset();
  mocks.virtCreate.mockResolvedValue(
    fakeJobSnapshot("job-create", "virt.create"),
  );
  mocks.virtDelete.mockReset();
  mocks.virtDelete.mockResolvedValue(
    fakeJobSnapshot("job-delete", "virt.delete"),
  );
  mocks.preflight = {
    ...mocks.readyPreflight,
    firmware: { ...mocks.readyPreflight.firmware },
    warnings: [],
  };
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
      message: "VM created",
      percent: 100,
      phase: "complete",
    });
    return { ...mocks.alpha, name: "created" };
  });
  Object.values(mocks.mutations).forEach((fn) => fn.mockReset());
});

describe("Virtual Machines page", () => {
  it("shows the libvirt capability warning when unavailable", () => {
    renderVMPage(false);

    expect(screen.getByText(/libvirt unavailable/i)).toBeInTheDocument();
    expect(
      screen.getByText(/libvirt daemon is unavailable/i),
    ).toBeInTheDocument();
  });

  it("renders the VM section tabs", () => {
    renderVMPage();

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
    const { user } = renderVMPage();

    await openVirtualMachinesTab(user);

    expect(screen.getByRole("button", { name: "alpha" })).toBeInTheDocument();
    expect(screen.getAllByText("192.168.122.57").length).toBeGreaterThanOrEqual(
      1,
    );
    expect(screen.getAllByText("running").length).toBeGreaterThanOrEqual(1);

    await user.click(screen.getByRole("button", { name: "Shutdown" }));

    expect(mocks.mutations.shutdown).toHaveBeenCalledWith({ name: "alpha" });
  });

  it("does not crash when delete success has no disk arrays", async () => {
    const { user } = renderVMPage();

    await openVirtualMachinesTab(user);
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

  it("does not show delete success when the delete job fails", async () => {
    mocks.waitForStreamResult.mockRejectedValueOnce(
      new Error("Domain not found: no domain with matching name 'alpha'"),
    );
    const { user } = renderVMPage();

    await openVirtualMachinesTab(user);
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

  it("allows create when the default storage pool can be repaired", () => {
    mocks.preflight = {
      ...mocks.readyPreflight,
      defaultPoolActive: false,
      defaultPoolExists: false,
      warnings: [
        "default storage pool is missing; create will define it at /var/lib/libvirt/images",
      ],
    };

    renderVMPage();

    expect(
      screen.getByText(/default storage pool is missing/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /create vm/i })).toBeEnabled();
  });

  it("submits the create dialog request shape", async () => {
    const { user } = renderVMPage();

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
    const { user } = renderVMPage();

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
    const { user } = renderVMPage();

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
    const { user } = renderVMPage();

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
    const { user } = renderVMPage();

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
          message: "Downloading Debian Server image (10.0 MiB / 100.0 MiB)",
          path: "/var/lib/libvirt/images/linuxio/cloud-images/linuxio-debian.qcow2.download",
          percent: 10,
          phase: "download",
        });
        return await new Promise((resolve) => {
          resolveCreate = resolve;
        });
      },
    );
    const { user } = renderVMPage();

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
    const { user } = renderVMPage();

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
    const { user } = renderVMPage();

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
      new Error("bad_request:no such file or directory"),
    );
    const { user } = renderVMPage();

    await user.click(screen.getByRole("button", { name: /create vm/i }));
    const dialog = screen.getByRole("dialog");

    await user.type(
      within(dialog).getByLabelText(/iso path/i),
      "/missing/debian.iso",
    );
    await user.tab();

    await waitFor(() => {
      expect(mocks.resourceStat).toHaveBeenCalledWith("/missing");
      expect(mocks.resourcePost).toHaveBeenCalledWith({ path: "/missing/" });
    });
  });

  it("opens the noVNC console over a mux stream", async () => {
    const { user } = renderVMPage();

    await openVirtualMachinesTab(user);
    await user.click(screen.getByRole("button", { name: "Console" }));

    expect(mocks.openVMConsoleStream).toHaveBeenCalledWith("alpha");
    await waitFor(() => expect(mocks.rfbConstructor).toHaveBeenCalled());
  });

  it("shows console stream result errors", async () => {
    const { user } = renderVMPage();

    await openVirtualMachinesTab(user);
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
