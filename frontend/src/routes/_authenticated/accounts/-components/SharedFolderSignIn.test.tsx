import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AccountUser } from "@/api";
import { renderWithTanStackRouter, screen, within } from "@/test/render";

import ChangePasswordDialog from "./ChangePasswordDialog";
import CreateUserDialog from "./CreateUserDialog";
import UserCard from "./UserCard";

const mocks = vi.hoisted(() => ({
  sambaUsers: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
  });
  const query = (key: string, fn: () => unknown) => ({
    queryKey: [key],
    queryFn: fn,
  });
  const mocked = {
    ...actual,
    useCallMutation,
    linuxio: {
      ...actual.linuxio,
      accounts: {
        ...actual.linuxio.accounts,
        list_shells: query("test-shells", () => Promise.resolve(["/bin/bash"])),
        list_groups: query("test-groups", () => Promise.resolve([])),
      },
      shares: {
        ...actual.linuxio.shares,
        list_samba_users: query("test-samba-users", () => mocks.sambaUsers()),
      },
    },
  };
  return {
    ...mocked,
    linuxio: mockMutationHooks(mocked.linuxio, useCallMutation),
  };
});

const SIGN_IN = "Can open shared folders from Windows and Mac";

const admin = {
  auth: { privileged: true },
  capabilities: { sambaServerAvailable: true },
};
const withoutSamba = {
  auth: { privileged: true },
  capabilities: { sambaServerAvailable: false },
};

const alice = {
  username: "alice",
  uid: 1000,
  gid: 1000,
  gecos: "Alice",
  groups: [],
  homeDir: "/home/alice",
  shell: "/bin/bash",
  primaryGroup: "alice",
  isLocked: false,
  isSystem: false,
  lastLogin: "",
} as AccountUser;

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.sambaUsers.mockResolvedValue([
    { username: "alice", passwordStale: false },
  ]);
});

const typePasswords = async (
  user: ReturnType<typeof userEvent.setup>,
  dialog: HTMLElement,
  first: string,
) => {
  await user.type(within(dialog).getByLabelText(new RegExp(`^${first}`)), "pw");
  await user.type(within(dialog).getByLabelText(/^Confirm Password/), "pw");
};

describe("ChangePasswordDialog", () => {
  it("keeps sign-in on for a user who has it", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <ChangePasswordDialog onClose={vi.fn()} open username="alice" />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByLabelText(SIGN_IN)).toBeChecked();
    await typePasswords(user, dialog, "New Password");
    await user.click(
      within(dialog).getByRole("button", { name: "Change Password" }),
    );
    expect(mocks.mutations["accounts.change_password"]).toHaveBeenCalledWith({
      username: "alice",
      password: "pw",
      smbSignIn: true,
    });
  });

  it("switches sign-in on with the new password", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <ChangePasswordDialog onClose={vi.fn()} open username="bob" />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    const box = await within(dialog).findByLabelText(SIGN_IN);
    expect(box).not.toBeChecked();
    await user.click(box);
    await typePasswords(user, dialog, "New Password");
    await user.click(
      within(dialog).getByRole("button", { name: "Change Password" }),
    );
    expect(mocks.mutations["accounts.change_password"]).toHaveBeenCalledWith({
      username: "bob",
      password: "pw",
      smbSignIn: true,
    });
  });

  it("says when the password was changed outside LinuxIO", async () => {
    mocks.sambaUsers.mockResolvedValue([
      { username: "alice", passwordStale: true },
    ]);
    renderWithTanStackRouter(
      <ChangePasswordDialog onClose={vi.fn()} open username="alice" />,
      admin,
    );
    expect(
      await screen.findByText(/was changed outside LinuxIO/),
    ).toBeInTheDocument();
  });

  it("turns sign-in off without a new password", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <ChangePasswordDialog onClose={vi.fn()} open username="alice" />,
      admin,
    );
    await user.click(
      await screen.findByRole("button", {
        name: "Turn off without changing the password",
      }),
    );
    expect(mocks.mutations["shares.remove_samba_user"]).toHaveBeenCalledWith({
      username: "alice",
    });
  });

  // The list has not arrived: saying "off" would take the user out of
  // Samba's list, so the server is left to keep whoever is in it in step.
  it("says nothing about sign-in before it knows the user's", async () => {
    const user = userEvent.setup();
    mocks.sambaUsers.mockReturnValue(new Promise(() => {}));
    renderWithTanStackRouter(
      <ChangePasswordDialog onClose={vi.fn()} open username="alice" />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    await typePasswords(user, dialog, "New Password");
    await user.click(
      within(dialog).getByRole("button", { name: "Change Password" }),
    );
    expect(mocks.mutations["accounts.change_password"]).toHaveBeenCalledWith({
      username: "alice",
      password: "pw",
    });
  });

  it("offers no sign-in without Samba", async () => {
    renderWithTanStackRouter(
      <ChangePasswordDialog onClose={vi.fn()} open username="alice" />,
      withoutSamba,
    );
    await screen.findByRole("dialog");
    expect(screen.queryByLabelText(SIGN_IN)).toBeNull();
    expect(mocks.sambaUsers).not.toHaveBeenCalled();
  });
});

describe("CreateUserDialog", () => {
  it("leaves sign-in off unless it is ticked", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <CreateUserDialog onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(/^Username/), "dave");
    await typePasswords(user, dialog, "Password");
    expect(within(dialog).getByLabelText(SIGN_IN)).not.toBeChecked();
    await user.click(within(dialog).getByRole("button", { name: /Create/ }));
    const sent = mocks.mutations["accounts.create_user"]?.mock.calls[0]?.[0];
    expect(sent).toMatchObject({ username: "dave", password: "pw" });
    expect(sent).not.toHaveProperty("smbSignIn");
  });

  it("switches sign-in on for the new user", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <CreateUserDialog onClose={vi.fn()} open />,
      admin,
    );
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(/^Username/), "dave");
    await typePasswords(user, dialog, "Password");
    await user.click(within(dialog).getByLabelText(SIGN_IN));
    await user.click(within(dialog).getByRole("button", { name: /Create/ }));
    expect(mocks.mutations["accounts.create_user"]).toHaveBeenCalledWith(
      expect.objectContaining({ username: "dave", smbSignIn: true }),
    );
  });
});

describe("UserCard", () => {
  const renderCard = () =>
    renderWithTanStackRouter(
      <UserCard
        currentUsername="root"
        onChangePassword={vi.fn()}
        onEdit={vi.fn()}
        onOpen={vi.fn()}
        onToggleLock={vi.fn()}
        user={alice}
      />,
      admin,
    );

  it("marks a user who can sign in to shared folders", async () => {
    renderCard();
    expect(await screen.findByText("Shared folders")).toBeInTheDocument();
  });

  it("flags a password changed outside LinuxIO", async () => {
    mocks.sambaUsers.mockResolvedValue([
      { username: "alice", passwordStale: true },
    ]);
    renderCard();
    expect(
      await screen.findByText("Shared folders: old password"),
    ).toBeInTheDocument();
  });
});
