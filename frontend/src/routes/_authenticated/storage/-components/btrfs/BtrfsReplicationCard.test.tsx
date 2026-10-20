import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  BtrfsReplicationJob,
  BtrfsReplicationResult,
  BtrfsSubvolume,
} from "@/api";
import { render } from "@/test/render";
import type { TaskStreamActionMockConfig } from "@/test/taskStreamAction";

import BtrfsReplicationCard from "./BtrfsReplicationCard";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  jobs: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
  runResult: undefined as unknown,
  submit: vi.fn(),
  subvolumes: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => ({
    error: vi.fn(),
    info: vi.fn(),
    success: mocks.toastSuccess,
    warning: vi.fn(),
  }),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { createTaskStreamActionMock } =
    await import("@/test/taskStreamAction");
  return {
    ...actual,
    call: (route: string) =>
      route === "tasks.list" ? Promise.resolve([]) : Promise.reject(),
    useCallMutation: (endpoint: { route: string }) => ({
      isPending: false,
      mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
    }),
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_subvolumes: (request: { uuid: string }) => ({
          queryKey: ["test-btrfs-subvolumes", request.uuid],
          queryFn: () => mocks.subvolumes(request),
        }),
        btrfs_replication_jobs: {
          queryKey: ["test-btrfs-replication-jobs"],
          queryFn: () => mocks.jobs(),
        },
        btrfs_replication_ssh_key: {
          queryKey: ["test-btrfs-replication-ssh-key"],
          queryFn: () =>
            Promise.resolve({ publicKey: "ssh-ed25519 AAAA test" }),
        },
        btrfs_replication_run: {
          useTaskStreamAction: (config?: TaskStreamActionMockConfig) => ({
            isPending: false,
            ...createTaskStreamActionMock(
              {
                openStream: () => ({}),
                runStream: () => Promise.resolve(mocks.runResult),
                submit: mocks.submit,
              },
              config,
            ),
          }),
        },
      },
    },
  };
});

const subvolume = (path: string, id: number): BtrfsSubvolume => ({
  id,
  parentId: 5,
  path,
  uuid: `u-${id}`,
  parentUuid: "",
  receivedUuid: "",
  readOnly: false,
  isDefault: false,
  isSnapshot: false,
});

const localJob: BtrfsReplicationJob = {
  id: "job-1",
  uuid: "uuid-1",
  path: "@",
  target: { kind: "local", path: "/mnt/backup" },
  keepOnTarget: 3,
  enabled: true,
  after: "timeline",
  lastRun: "2026-10-19T10:00:00Z",
};
const sshJob: BtrfsReplicationJob = {
  id: "job-2",
  uuid: "uuid-1",
  path: "data",
  target: {
    kind: "ssh",
    host: "nas.lan",
    port: 22,
    user: "root",
    path: "/backups/data",
  },
  keepOnTarget: 0,
  enabled: false,
  after: "manual",
  lastError: "connection refused",
};
const otherFilesystemJob: BtrfsReplicationJob = {
  ...localJob,
  id: "job-3",
  uuid: "uuid-2",
  path: "elsewhere",
};

const route = (name: string) => {
  const mutate = mocks.mutations[`storage.${name}`];
  if (!mutate) throw new Error(`no mutation registered for ${name}`);
  return mutate;
};

const rowFor = async (text: string) =>
  (await screen.findByText(text)).closest("[role=row]") as HTMLElement;

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.runResult = { upToDate: true, bytes: 0, pruned: 0 };
  mocks.submit.mockResolvedValue({ id: "task-1" });
  mocks.jobs.mockResolvedValue([localJob, sshJob, otherFilesystemJob]);
  mocks.subvolumes.mockResolvedValue([
    subvolume("@", 256),
    subvolume("@home", 257),
    { ...subvolume("linuxio-snapshots/@-1", 300), isSnapshot: true },
  ]);
});

describe("BtrfsReplicationCard", () => {
  it("lists this filesystem's jobs with target, last run and status", async () => {
    render(<BtrfsReplicationCard uuid="uuid-1" />);

    const local = await rowFor("local:/mnt/backup");
    expect(within(local).getByText("OK")).toBeInTheDocument();
    const ssh = await rowFor("root@nas.lan:/backups/data");
    expect(within(ssh).getByText("Error")).toBeInTheDocument();
    expect(within(ssh).getByText("Never")).toBeInTheDocument();
    expect(screen.queryByText("elsewhere")).not.toBeInTheDocument();
  });

  it("creates an ssh job", async () => {
    const user = userEvent.setup();
    render(<BtrfsReplicationCard uuid="uuid-1" />);
    await rowFor("local:/mnt/backup");

    await user.click(screen.getByRole("button", { name: "Add job" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    await user.click(
      within(dialog).getByRole("combobox", { name: "Source subvolume" }),
    );
    await user.click(screen.getByRole("option", { name: "@home" }));
    await user.click(
      within(dialog).getByRole("combobox", { name: "Target kind" }),
    );
    await user.click(screen.getByRole("option", { name: "SSH" }));
    await user.type(within(dialog).getByLabelText("Host"), "nas.lan");
    await user.type(within(dialog).getByLabelText("User"), "root");
    await user.type(
      within(dialog).getByLabelText("Target path"),
      "/backups/home",
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(route("btrfs_replication_job_set")).toHaveBeenCalledWith(
      {
        job: {
          id: "",
          uuid: "uuid-1",
          path: "@home",
          target: {
            kind: "ssh",
            host: "nas.lan",
            port: 22,
            user: "root",
            path: "/backups/home",
          },
          keepOnTarget: 5,
          enabled: true,
          after: "timeline",
        },
      },
      expect.anything(),
    );
  });

  it("trusts exactly the fingerprints it showed", async () => {
    const user = userEvent.setup();
    mocks.mutations["storage.btrfs_replication_ssh_trust"] = vi.fn(
      (
        request: { confirm: boolean },
        options: { onSuccess: (result: unknown) => void },
      ) =>
        options.onSuccess({
          fingerprints: ["SHA256:abc"],
          trusted: request.confirm,
        }),
    );
    render(<BtrfsReplicationCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("root@nas.lan:/backups/data")).getByRole("button", {
        name: "Edit",
      }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(
      await within(dialog).findByDisplayValue("ssh-ed25519 AAAA test"),
    ).toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "Check host key" }),
    );
    expect(await within(dialog).findByText("SHA256:abc")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Trust" }));

    const trust = route("btrfs_replication_ssh_trust");
    expect(trust).toHaveBeenNthCalledWith(
      1,
      { host: "nas.lan", port: 22, confirm: false },
      expect.anything(),
    );
    expect(trust).toHaveBeenNthCalledWith(
      2,
      {
        host: "nas.lan",
        port: 22,
        confirm: true,
        fingerprints: ["SHA256:abc"],
      },
      expect.anything(),
    );
    expect(await within(dialog).findByText("Host key trusted")).toBeVisible();
  });

  it("confirms before deleting", async () => {
    const user = userEvent.setup();
    render(<BtrfsReplicationCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("local:/mnt/backup")).getByRole("button", {
        name: "Delete",
      }),
    );
    expect(route("btrfs_replication_job_delete")).not.toHaveBeenCalled();
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Delete",
      }),
    );

    expect(route("btrfs_replication_job_delete")).toHaveBeenCalledWith(
      { id: "job-1" },
      expect.anything(),
    );
  });

  it("reports an up-to-date target after Run now", async () => {
    const user = userEvent.setup();
    mocks.runResult = {
      upToDate: true,
      bytes: 0,
      pruned: 0,
    } satisfies BtrfsReplicationResult;
    render(<BtrfsReplicationCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("local:/mnt/backup")).getByRole("button", {
        name: "Run now",
      }),
    );

    expect(mocks.submit).toHaveBeenCalledWith({ id: "job-1" });
    await waitFor(() =>
      expect(mocks.toastSuccess).toHaveBeenCalledWith("Target is up to date"),
    );
  });
});
