import { act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AccountGroup, AccountUser, SharedFolder } from "@/api";
import { renderWithTanStackRouter, screen, within } from "@/test/render";

import { poolFixture } from "../pools/poolTestFixtures";
import DeleteSharedFolderDialog from "./DeleteSharedFolderDialog";
import SharedFolderDialog from "./SharedFolderDialog";
import StopSharingDialog from "./StopSharingDialog";

type MutationConfig = { error?: (error: unknown) => void };

const mocks = vi.hoisted(() => ({
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
  configs: {} as Record<string, MutationConfig>,
  sambaUsers: vi.fn(),
  users: vi.fn(),
  subvolumes: vi.fn(),
  snapshots: vi.fn(),
  jobs: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const { poolFixture: pool } = await import("../pools/poolTestFixtures");
  const useCallMutation = (
    endpoint: { route: string },
    config: MutationConfig,
  ) => {
    mocks.configs[endpoint.route] = config;
    return {
      isPending: false,
      mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
    };
  };
  const query = (key: string, fn: () => unknown) => ({
    queryKey: [key],
    queryFn: fn,
  });
  const groups = [
    { name: "family", gid: 2000, members: ["alice", "bob"] },
  ] as AccountGroup[];
  const mocked = {
    ...actual,
    useCallMutation,
    linuxio: {
      ...actual.linuxio,
      accounts: {
        ...actual.linuxio.accounts,
        list_users: query("test-users", () => mocks.users()),
        list_groups: query("test-groups", () => Promise.resolve(groups)),
      },
      shares: {
        ...actual.linuxio.shares,
        list_samba_users: query("test-samba-users", () => mocks.sambaUsers()),
      },
      storage: {
        ...actual.linuxio.storage,
        btrfs_list: query("test-btrfs-list", () => Promise.resolve([pool()])),
        btrfs_subvolumes: () => query("test-subvols", () => mocks.subvolumes()),
        btrfs_snapshots: () => query("test-snaps", () => mocks.snapshots()),
        btrfs_replication_jobs: query("test-jobs", () => mocks.jobs()),
      },
    },
  };
  return {
    ...mocked,
    linuxio: mockMutationHooks(mocked.linuxio, useCallMutation as never),
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
  nfs: [{ host: "10.0.0.0/24", options: ["rw", "sync", "no_subtree_check"] }],
  folder: "/mnt/tank/Media",
  previousVersions: false,
  recycleBin: false,
  timeMachine: false,
};

const admin = {
  auth: { privileged: true, user: { id: "alice", name: "alice" } },
  capabilities: {
    btrfsAvailable: true,
    nfsServerAvailable: true,
    sambaServerAvailable: true,
  },
};

const users = [
  { username: "root", uid: 0, gecos: "" },
  { username: "alice", uid: 1000, gecos: "Alice" },
  { username: "bob", uid: 1001, gecos: "" },
] as AccountUser[];

beforeEach(() => {
  vi.clearAllMocks();
  mocks.users.mockResolvedValue(users);
  mocks.mutations = {};
  mocks.configs = {};
  mocks.sambaUsers.mockResolvedValue([
    { username: "alice", passwordStale: false },
  ]);
  mocks.subvolumes.mockResolvedValue([
    { path: "Media", referencedBytes: 5 * 1024 ** 3 },
  ]);
  mocks.snapshots.mockResolvedValue([{ path: "a" }, { path: "b" }]);
  mocks.jobs.mockResolvedValue([]);
});

const pickOption = async (
  user: ReturnType<typeof userEvent.setup>,
  field: string,
  option: string | RegExp,
) => {
  await user.click(await screen.findByRole("combobox", { name: field }));
  await user.click(await screen.findByRole("option", { name: option }));
};

describe("SharedFolderDialog", () => {
  it("creates a folder, its limit, its snapshots, its access and its shares in one request", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={null} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Create shared folder",
    });
    await pickOption(user, "Pool", new RegExp(poolFixture().label));
    await user.type(within(dialog).getByLabelText("Folder name"), "Media");
    await user.type(within(dialog).getByLabelText("Size limit (GiB)"), "2");
    await user.click(within(dialog).getByRole("button", { name: "Create" }));

    expect(mocks.mutations["shares.create_folder"]).toHaveBeenCalledWith({
      uuid: "uuid-tank",
      name: "Media",
      maxReferencedBytes: 2 * 1024 ** 3,
      snapshotRule: expect.objectContaining({
        uuid: "uuid-tank",
        path: "Media",
        enabled: true,
        every: "hourly",
      }),
      // The administrator making it, and nobody else.
      access: [{ kind: "user", name: "alice", level: "rw" }],
      everyone: "none",
      smb: true,
      nfs: [],
      // With automatic snapshots, Windows and Macs offer them as earlier
      // versions.
      previousVersions: true,
      recycleBin: true,
      timeMachine: false,
    });
    // The page no longer makes the folder step by step.
    expect(mocks.mutations["storage.btrfs_subvolume_create"]).toBeUndefined();
  });

  it("refuses a folder nobody could open over SMB, without asking the server", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={null} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    await pickOption(user, "Pool", new RegExp(poolFixture().label));
    await user.type(within(dialog).getByLabelText("Folder name"), "Media");
    await user.click(within(dialog).getByRole("tab", { name: "Access" }));
    await user.click(
      within(
        await within(dialog).findByRole("radiogroup", {
          name: "Access for alice",
        }),
      ).getByRole("radio", { name: "No access" }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Create" }));
    expect(
      within(dialog).getByText("Give someone access before sharing over SMB"),
    ).toBeInTheDocument();
    expect(mocks.mutations["shares.create_folder"]).not.toHaveBeenCalled();
  });

  it("renames the share and keeps the folder where it is", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    expect(
      within(dialog).getByText(
        /\/mnt\/tank\/Media · \\\\linuxio\.test\\Media on Windows/,
      ),
    ).toBeInTheDocument();
    const name = within(dialog).getByLabelText("Share name");
    await user.clear(name);
    await user.type(name, "Films");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(mocks.mutations["shares.update_folder"]).toHaveBeenCalledWith({
      uuid: "uuid-tank",
      path: "Media",
      name: "Films",
      access: media.access,
      everyone: "none",
      smb: true,
      // Written out in full, the squash default included.
      nfs: [
        {
          host: "10.0.0.0/24",
          options: ["rw", "sync", "no_subtree_check", "root_squash"],
        },
      ],
      previousVersions: false,
      recycleBin: false,
      timeMachine: false,
    });
  });

  it("offers earlier versions only when the folder takes snapshots", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={null} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    await pickOption(user, "Pool", new RegExp(poolFixture().label));
    await user.type(within(dialog).getByLabelText("Folder name"), "Media");
    await user.click(within(dialog).getByLabelText(/Automatic snapshots/));
    await user.click(within(dialog).getByRole("tab", { name: "Network" }));
    expect(
      within(dialog).getByLabelText(
        "Windows and Mac can open earlier versions from snapshots",
      ),
    ).not.toBeChecked();
    await user.click(within(dialog).getByRole("button", { name: "Create" }));
    expect(mocks.mutations["shares.create_folder"]).toHaveBeenCalledWith(
      expect.objectContaining({ previousVersions: false }),
    );
  });

  it("keeps a recycle bin for a new folder unless told not to", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={null} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    await pickOption(user, "Pool", new RegExp(poolFixture().label));
    await user.type(within(dialog).getByLabelText("Folder name"), "Media");
    await user.click(within(dialog).getByRole("tab", { name: "Network" }));
    const bin = within(dialog).getByLabelText(
      "Keep files deleted over the network in a recycle bin for 30 days",
    );
    expect(bin).toBeChecked();
    await user.click(bin);
    await user.click(within(dialog).getByRole("button", { name: "Create" }));
    expect(mocks.mutations["shares.create_folder"]).toHaveBeenCalledWith(
      expect.objectContaining({ recycleBin: false, previousVersions: true }),
    );
  });

  it("offers the folder to Macs for Time Machine, with a size its backups may take", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    await user.click(within(dialog).getByRole("tab", { name: "Network" }));
    expect(
      within(dialog).queryByLabelText("Time Machine size limit (GiB)"),
    ).toBeNull();
    expect(within(dialog).queryByText(/restarts the SMB service/)).toBeNull();
    await user.click(
      within(dialog).getByLabelText("Use as a Time Machine backup destination"),
    );
    // Macs are told of it only when the service starts.
    expect(
      within(dialog).getByText(/restarts the SMB service/),
    ).toBeInTheDocument();
    await user.type(
      within(dialog).getByLabelText("Time Machine size limit (GiB)"),
      "500",
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(mocks.mutations["shares.update_folder"]).toHaveBeenCalledWith(
      expect.objectContaining({
        timeMachine: true,
        timeMachineMaxBytes: 500 * 1024 ** 3,
      }),
    );
  });

  it("says that renaming a Time Machine folder restarts the service too", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog
        folder={{ ...media, timeMachine: true }}
        onClose={vi.fn()}
        open
      />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    expect(within(dialog).queryByText(/restarts the SMB service/)).toBeNull();
    await user.type(within(dialog).getByLabelText("Share name"), "2");
    expect(
      within(dialog).getByText(/restarts the SMB service/),
    ).toBeInTheDocument();
  });

  it("refuses a Time Machine size too small to be one", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    await user.click(within(dialog).getByRole("tab", { name: "Network" }));
    await user.click(
      within(dialog).getByLabelText("Use as a Time Machine backup destination"),
    );
    await user.type(
      within(dialog).getByLabelText("Time Machine size limit (GiB)"),
      "0.0000000001",
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(mocks.mutations["shares.update_folder"]).not.toHaveBeenCalled();
  });

  // A folder shared over NFS alone keeps a bin that is emptied each night:
  // its switch must still turn off.
  it("lets a switch that is on be turned off while the folder is not shared over SMB", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog
        folder={{ ...media, smb: false, recycleBin: true, timeMachine: false }}
        onClose={vi.fn()}
        open
      />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    await user.click(within(dialog).getByRole("tab", { name: "Network" }));
    expect(
      within(dialog).getByLabelText("Use as a Time Machine backup destination"),
    ).toBeDisabled();
    const bin = within(dialog).getByLabelText(
      "Keep files deleted over the network in a recycle bin for 30 days",
    );
    expect(bin).toBeEnabled();
    await user.click(bin);
    expect(bin).toBeDisabled();
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(mocks.mutations["shares.update_folder"]).toHaveBeenCalledWith(
      expect.objectContaining({ smb: false, recycleBin: false }),
    );
  });

  it("shows a saved Time Machine size as it was typed", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog
        folder={{
          ...media,
          timeMachine: true,
          timeMachineMaxBytes: Math.round(0.3 * 1024 ** 3),
        }}
        onClose={vi.fn()}
        open
      />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    await user.click(within(dialog).getByRole("tab", { name: "Network" }));
    expect(
      within(dialog).getByLabelText("Time Machine size limit (GiB)"),
    ).toHaveValue(0.3);
  });

  it("gives a new folder that is not shared over SMB none of what SMB offers", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={null} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    await pickOption(user, "Pool", new RegExp(poolFixture().label));
    await user.type(within(dialog).getByLabelText("Folder name"), "Media");
    await user.click(
      within(dialog).getByLabelText("Share with Windows and Mac (SMB)"),
    );
    await user.click(within(dialog).getByRole("button", { name: "Create" }));
    expect(mocks.mutations["shares.create_folder"]).toHaveBeenCalledWith(
      expect.objectContaining({
        smb: false,
        previousVersions: false,
        recycleBin: false,
        timeMachine: false,
      }),
    );
  });

  it("refuses a Time Machine size that is not a size", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    await user.click(within(dialog).getByRole("tab", { name: "Network" }));
    await user.click(
      within(dialog).getByLabelText("Use as a Time Machine backup destination"),
    );
    await user.type(
      within(dialog).getByLabelText("Time Machine size limit (GiB)"),
      "-3",
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(
      within(dialog).getByText(
        "Time Machine size limit must be a positive number of GiB",
      ),
    ).toBeInTheDocument();
    expect(mocks.mutations["shares.update_folder"]).not.toHaveBeenCalled();
  });

  it("switches earlier versions on for a folder", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    await user.click(within(dialog).getByRole("tab", { name: "Network" }));
    await user.click(
      within(dialog).getByLabelText(
        "Windows and Mac can open earlier versions from snapshots",
      ),
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(mocks.mutations["shares.update_folder"]).toHaveBeenCalledWith(
      expect.objectContaining({ previousVersions: true }),
    );
  });

  it("starts a folder made by root with nobody, and shows the Access tab when Create needs a row", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={null} onClose={vi.fn()} open />,
      {
        ...admin,
        auth: { privileged: true, user: { id: "root", name: "root" } },
      },
    );
    const dialog = await screen.findByRole("dialog");
    await pickOption(user, "Pool", new RegExp(poolFixture().label));
    await user.type(within(dialog).getByLabelText("Folder name"), "Media");
    await user.click(within(dialog).getByRole("button", { name: "Create" }));
    expect(mocks.mutations["shares.create_folder"]).not.toHaveBeenCalled();
    expect(within(dialog).getByRole("tab", { name: "Access" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(
      within(dialog).getByRole("radiogroup", {
        name: "Access for everyone else",
      }),
    ).toBeInTheDocument();
  });

  it("warns that lowering access leaves what is inside as it was", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <SharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    await user.click(within(dialog).getByRole("tab", { name: "Access" }));
    expect(within(dialog).queryByText(/already inside keep/)).toBeNull();
    await user.click(
      within(
        await within(dialog).findByRole("radiogroup", {
          name: "Access for alice",
        }),
      ).getByRole("radio", { name: "Read only" }),
    );
    expect(within(dialog).getByText(/already inside keep/)).toBeInTheDocument();
  });

  it("says why Create waits when the accounts cannot be read", async () => {
    mocks.users.mockRejectedValue(new Error("boom"));
    renderWithTanStackRouter(
      <SharedFolderDialog folder={null} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    expect(
      await within(dialog).findByText(/The users and groups could not be read/),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Create" }),
    ).toBeDisabled();
  });

  it("shows what the server refused, in the dialog", async () => {
    renderWithTanStackRouter(
      <SharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    act(() =>
      mocks.configs["shares.update_folder"]?.error?.(
        new Error("a share is already named Films"),
      ),
    );
    expect(
      await within(dialog).findByText(/a share is already named Films/),
    ).toBeInTheDocument();
  });

  it("says who with access cannot sign in from Windows or a Mac yet", async () => {
    const user = userEvent.setup();
    mocks.sambaUsers.mockResolvedValue([]);
    renderWithTanStackRouter(
      <SharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Media" });
    await user.click(within(dialog).getByRole("tab", { name: "Access" }));
    expect(
      await within(dialog).findByText(/alice cannot sign in from Windows/),
    ).toBeInTheDocument();
  });
});

describe("StopSharingDialog", () => {
  it("takes the folder off the network and keeps its access", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <StopSharingDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", { name: "Stop sharing" });
    await user.click(
      within(dialog).getByRole("button", { name: "Stop sharing" }),
    );
    expect(mocks.mutations["shares.update_folder"]).toHaveBeenCalledWith({
      uuid: "uuid-tank",
      path: "Media",
      name: "Media",
      access: media.access,
      everyone: "none",
      smb: false,
      nfs: [],
      previousVersions: false,
      recycleBin: false,
      timeMachine: false,
      timeMachineMaxBytes: undefined,
    });
  });
});

describe("a Time Machine folder taken off the network", () => {
  const backups = { ...media, timeMachine: true };

  it("says that Stop sharing restarts the service", async () => {
    renderWithTanStackRouter(
      <StopSharingDialog folder={backups} onClose={vi.fn()} open />,
      admin,
    );
    expect(
      await screen.findByText(/restarts the SMB service/),
    ).toBeInTheDocument();
  });

  it("says that Delete restarts the service", async () => {
    renderWithTanStackRouter(
      <DeleteSharedFolderDialog folder={backups} onClose={vi.fn()} open />,
      admin,
    );
    expect(
      await screen.findByText(/restarts the SMB service/),
    ).toBeInTheDocument();
  });

  it("says nothing of a restart for a folder that is not one", async () => {
    renderWithTanStackRouter(
      <StopSharingDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    await screen.findByRole("dialog", { name: "Stop sharing" });
    expect(screen.queryByText(/restarts the SMB service/)).toBeNull();
  });
});

describe("DeleteSharedFolderDialog", () => {
  it("removes only the share and the record of a folder whose pool is not mounted", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <DeleteSharedFolderDialog
        folder={{ ...media, folder: "" }}
        onClose={vi.fn()}
        open
      />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Remove shared folder",
    });
    expect(
      within(dialog).getByText(
        /The pool of Media is not mounted, so only its share, its snapshot schedule and its record are removed\. What is on the pool stays/,
      ),
    ).toBeInTheDocument();
    expect(mocks.subvolumes).not.toHaveBeenCalled();
    await user.type(
      within(dialog).getByLabelText("Type Media to confirm"),
      "Media",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Remove shared folder" }),
    );
    expect(mocks.mutations["shares.delete_folder"]).toHaveBeenCalledWith({
      uuid: "uuid-tank",
      path: "Media",
      confirmName: "Media",
    });
  });

  it("says what goes and deletes only once the folder's name is typed", async () => {
    const user = userEvent.setup();
    mocks.jobs.mockResolvedValue([{ uuid: "uuid-tank", path: "Media" }]);
    renderWithTanStackRouter(
      <DeleteSharedFolderDialog folder={media} onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Delete folder and its data",
    });
    expect(
      await within(dialog).findByText(
        /\/mnt\/tank\/Media is deleted with everything in it \(5 GB\) and its 2 snapshots\. This cannot be undone\. Its copies on another pool or server stay/,
      ),
    ).toBeInTheDocument();
    const confirm = within(dialog).getByRole("button", {
      name: "Delete folder and its data",
    });
    expect(confirm).toBeDisabled();
    const field = within(dialog).getByLabelText("Type Media to confirm");
    await user.type(field, "media");
    expect(confirm).toBeDisabled();
    await user.clear(field);
    await user.type(field, "Media");
    await user.click(confirm);
    expect(mocks.mutations["shares.delete_folder"]).toHaveBeenCalledWith({
      uuid: "uuid-tank",
      path: "Media",
      confirmName: "Media",
    });
  });
});
