import { afterEach, describe, expect, it, vi } from "vitest";

import { linuxio, type CIFSMount, type NFSMount } from "@/api";
import * as core from "@/api/linuxio-core";
import {
  act,
  createTestQueryClient,
  render,
  screen,
  waitFor,
  within,
} from "@/test/render";

import CIFSMounts from "./CIFSMounts";
import NFSMounts from "./NFSMounts";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({ reason: "", status: "available" }),
}));

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
}

function createDeferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function nfsMount(
  mountpoint: string,
  mounted: boolean,
  server: string,
): NFSMount {
  return {
    exportPath: "/exports/data",
    free: mounted ? 750 : 0,
    fsType: "nfs4",
    inFstab: true,
    mounted,
    mountpoint,
    options: ["rw"],
    server,
    size: mounted ? 1000 : 0,
    source: `${server}:/exports/data`,
    used: mounted ? 250 : 0,
    usedPct: mounted ? 25 : 0,
  };
}

function cifsMount(
  mountpoint: string,
  mounted: boolean,
  server: string,
): CIFSMount {
  return {
    domain: "",
    free: mounted ? 750 : 0,
    fsType: "cifs",
    inFstab: true,
    mounted,
    mountpoint,
    options: ["rw"],
    server,
    share: "data",
    size: mounted ? 1000 : 0,
    source: `//${server}/data`,
    used: mounted ? 250 : 0,
    usedPct: mounted ? 25 : 0,
    username: "linuxio",
  };
}

describe("mount mutation feedback", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("keeps NFS mount and unmount progress scoped to their entries", async () => {
    const mounting = createDeferred<{
      success: boolean;
      mountpoint?: string;
    }>();
    const unmounting = createDeferred<{ success: boolean }>();
    const mounts = [
      nfsMount("/mnt/alpha", false, "nas-a"),
      nfsMount("/mnt/beta", true, "nas-b"),
    ];
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(linuxio.storage.list_nfs_mounts.queryKey, mounts);
    vi.spyOn(core, "request").mockImplementation(
      (_handler, command, request) => {
        const mountpoint = (request as { mountpoint?: string } | undefined)
          ?.mountpoint;
        if (command === "mount_nfs" && mountpoint === "/mnt/alpha") {
          return mounting.promise;
        }
        if (command === "unmount_nfs" && mountpoint === "/mnt/beta") {
          return unmounting.promise;
        }
        if (command === "list_nfs_mounts") {
          return Promise.resolve(mounts);
        }
        return Promise.resolve();
      },
    );
    const view = render(<NFSMounts />, { queryClient });
    const alphaActions = within(
      await screen.findByRole("group", { name: "Actions for /mnt/alpha" }),
    );
    const betaActions = within(
      screen.getByRole("group", { name: "Actions for /mnt/beta" }),
    );

    await view.user.click(
      alphaActions.getByRole("button", { name: "Mount entry" }),
    );

    expect(
      within(
        await screen.findByRole("button", {
          name: "Mounting /mnt/alpha",
        }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();
    expect(
      within(
        screen.getByRole("group", { name: "Actions for /mnt/alpha" }),
      ).getByRole("button", { name: "Edit entry" }),
    ).toBeDisabled();
    expect(
      betaActions.getByRole("button", { name: "Unmount entry" }),
    ).toBeEnabled();

    await view.user.click(
      within(
        screen.getByRole("group", { name: "Actions for /mnt/beta" }),
      ).getByRole("button", { name: "Unmount entry" }),
    );
    expect(
      within(
        await screen.findByRole("button", {
          name: "Unmounting /mnt/beta",
        }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();

    await act(async () => {
      mounting.resolve({ success: true, mountpoint: "/mnt/alpha" });
      await mounting.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Mounting /mnt/alpha" }),
      ).not.toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "Unmounting /mnt/beta" }),
    ).toBeInTheDocument();

    await act(async () => {
      unmounting.resolve({ success: true });
      await unmounting.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Unmounting /mnt/beta" }),
      ).not.toBeInTheDocument();
    });
  });

  it("keeps SMB mount and unmount progress scoped to their entries", async () => {
    const mounting = createDeferred<{
      success: boolean;
      mountpoint?: string;
    }>();
    const unmounting = createDeferred<{ success: boolean }>();
    const mounts = [
      cifsMount("/mnt/share-a", false, "smb-a"),
      cifsMount("/mnt/share-b", true, "smb-b"),
    ];
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(linuxio.storage.list_cifs_mounts.queryKey, mounts);
    const request = vi
      .spyOn(core, "request")
      .mockImplementation((_handler, command, request) => {
        const mountpoint = (request as { mountpoint?: string } | undefined)
          ?.mountpoint;
        if (command === "mount_cifs") {
          return mounting.promise;
        }
        if (command === "unmount_cifs" && mountpoint === "/mnt/share-b") {
          return unmounting.promise;
        }
        if (command === "list_cifs_mounts") {
          return Promise.resolve(mounts);
        }
        return Promise.resolve();
      });
    const view = render(<CIFSMounts />, { queryClient });
    const alphaActions = within(
      await screen.findByRole("group", { name: "Actions for /mnt/share-a" }),
    );
    const betaActions = within(
      screen.getByRole("group", { name: "Actions for /mnt/share-b" }),
    );

    const mountButton = alphaActions.getByRole("button", { name: "Mount" });
    expect(mountButton).toBeEnabled();
    await view.user.click(mountButton);

    expect(
      screen.getByRole("group", { name: "Actions for /mnt/share-a" }),
    ).toHaveAttribute("aria-busy", "true");

    expect(request).toHaveBeenCalledWith(
      "storage",
      "mount_cifs",
      expect.objectContaining({ mountpoint: "/mnt/share-a" }),
      { retryPolicy: "none" },
    );

    expect(
      within(
        await screen.findByRole("button", {
          name: "Mounting /mnt/share-a",
        }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();
    expect(
      within(
        screen.getByRole("group", { name: "Actions for /mnt/share-a" }),
      ).getByRole("button", { name: "Edit options" }),
    ).toBeDisabled();
    expect(betaActions.getByRole("button", { name: "Unmount" })).toBeEnabled();

    await view.user.click(
      within(
        screen.getByRole("group", { name: "Actions for /mnt/share-b" }),
      ).getByRole("button", { name: "Unmount" }),
    );
    expect(
      within(
        await screen.findByRole("button", {
          name: "Unmounting /mnt/share-b",
        }),
      ).getByRole("progressbar"),
    ).toBeInTheDocument();

    await act(async () => {
      mounting.resolve({ success: true, mountpoint: "/mnt/share-a" });
      await mounting.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Mounting /mnt/share-a" }),
      ).not.toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "Unmounting /mnt/share-b" }),
    ).toBeInTheDocument();

    await act(async () => {
      unmounting.resolve({ success: true });
      await unmounting.promise;
    });
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Unmounting /mnt/share-b" }),
      ).not.toBeInTheDocument();
    });
  });
});
