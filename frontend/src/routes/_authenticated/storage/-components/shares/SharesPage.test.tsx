import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { SharedFolder } from "@/api";
import { renderWithTanStackRouter, screen, within } from "@/test/render";

import SharesPage from "./SharesPage";

const mocks = vi.hoisted(() => ({
  folders: vi.fn(),
  samba: vi.fn(),
  nfs: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
    mutateAsync: (mocks.mutations[endpoint.route] ??= vi.fn()),
  });
  const query = (key: string, fn: () => unknown) => ({
    queryKey: [key],
    queryFn: fn,
  });
  const none = (key: string) => query(key, () => Promise.resolve([]));
  const mocked = {
    ...actual,
    useCallMutation,
    linuxio: {
      ...actual.linuxio,
      accounts: {
        ...actual.linuxio.accounts,
        list_users: none("test-users"),
        list_groups: none("test-groups"),
      },
      shares: {
        ...actual.linuxio.shares,
        list_folders: query("test-folders", () => mocks.folders()),
        list_samba_shares: query("test-samba", () => mocks.samba()),
        list_nfs_shares: query("test-nfs", () => mocks.nfs()),
        list_samba_users: none("test-samba-users"),
      },
      storage: {
        ...actual.linuxio.storage,
        btrfs_list: none("test-btrfs-list"),
        btrfs_subvolumes: () => none("test-subvols"),
        btrfs_snapshots: () => none("test-snaps"),
        btrfs_replication_jobs: none("test-jobs"),
      },
    },
  };
  return {
    ...mocked,
    linuxio: mockMutationHooks(mocked.linuxio, useCallMutation),
  };
});

const media: SharedFolder = {
  uuid: "uuid-tank",
  path: "Media",
  name: "Media",
  access: [
    { kind: "user", name: "alice", level: "rw" },
    { kind: "group", name: "family", level: "ro" },
  ],
  everyone: "none",
  smb: true,
  nfs: [],
  folder: "/mnt/tank/Media",
};

const archive: SharedFolder = {
  ...media,
  path: "Archive",
  name: "Archive",
  access: [],
  smb: false,
  folder: "/mnt/tank/Archive",
};

const admin = {
  auth: { privileged: true, user: { id: "alice", name: "alice" } },
  capabilities: {
    btrfsAvailable: true,
    nfsServerAvailable: true,
    sambaServerAvailable: true,
  },
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.folders.mockResolvedValue([media, archive]);
  mocks.samba.mockResolvedValue([
    { name: "Media", properties: { path: "/mnt/tank/Media" } },
    { name: "plain", properties: { path: "/srv/plain", comment: "old" } },
  ]);
  mocks.nfs.mockResolvedValue([]);
});

/** The card of a share, found by its path. */
const cardOf = async (path: string) => {
  const text = await screen.findByText(path);
  const card = text.closest<HTMLElement>(".frosted-card, [class*='card']");
  if (!card) throw new Error(`no card around ${path}`);
  return card;
};

describe("SharesPage", () => {
  it("lists a shared folder once, with who can use it", async () => {
    renderWithTanStackRouter(<SharesPage />, admin);
    expect(
      await screen.findByText("2 people and groups have access"),
    ).toBeInTheDocument();
    // The folder and its section are one row, not two.
    expect(screen.getAllByText("/mnt/tank/Media")).toHaveLength(1);
    // A folder that is not shared still has its row.
    expect(screen.getByText("/mnt/tank/Archive")).toBeInTheDocument();
    expect(screen.getByText("Not shared")).toBeInTheDocument();
    expect(screen.getByText("/srv/plain")).toBeInTheDocument();
  });

  it("offers a shared folder Stop sharing and Delete, not the removal of its section", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(<SharesPage />, admin);
    const card = await cardOf("/mnt/tank/Media");
    await user.click(
      within(card).getByRole("button", { name: "Remove Share" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "Stop sharing" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Remove SMB" })).toBeNull();
    await user.click(
      screen.getByRole("menuitem", { name: "Delete folder and its data" }),
    );
    expect(
      await screen.findByRole("dialog", { name: "Delete folder and its data" }),
    ).toBeInTheDocument();
  });

  it("opens a shared folder's own dialog to change it", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(<SharesPage />, admin);
    const card = await cardOf("/mnt/tank/Media");
    await user.click(within(card).getByRole("button", { name: "Edit Share" }));
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    expect(
      within(dialog).getByRole("tab", { name: "Access" }),
    ).toBeInTheDocument();
  });

  it("keeps a plain share's own actions", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(<SharesPage />, admin);
    const card = await cardOf("/srv/plain");
    await user.click(
      within(card).getByRole("button", { name: "Remove Share" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "Remove SMB" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Stop sharing" })).toBeNull();
  });

  it("adds a shared folder or a share of a folder that exists", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(<SharesPage />, admin);
    await screen.findByText("/srv/plain");
    await user.click(screen.getByRole("button", { name: "Add Share" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "Create shared folder" }),
    );
    expect(
      await screen.findByRole("dialog", { name: "Create shared folder" }),
    ).toBeInTheDocument();
  });

  it("leaves shared folders out for a session that cannot read their record", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(<SharesPage />, {
      ...admin,
      auth: { privileged: false },
    });
    await screen.findByText("/srv/plain");
    expect(mocks.folders).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Add Share" }));
    expect(
      await screen.findByRole("dialog", { name: "Share an existing folder" }),
    ).toBeInTheDocument();
  });
});
