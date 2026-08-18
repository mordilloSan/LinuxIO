import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { render } from "@/test/render";

import UsersTab from "./UsersTab";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();

  return {
    ...actual,
    useSuspenseQuery: () => ({
      data: [
        {
          gecos: "Alice",
          gid: 1000,
          groups: ["users"],
          homeDir: "/home/alice",
          isLocked: false,
          isSystem: false,
          lastLogin: null,
          primaryGroup: "users",
          shell: "/bin/bash",
          uid: 1000,
          username: "alice",
        },
        {
          gecos: "Bob",
          gid: 1001,
          groups: ["users"],
          homeDir: "/home/bob",
          isLocked: true,
          isSystem: false,
          lastLogin: null,
          primaryGroup: "users",
          shell: "/bin/bash",
          uid: 1001,
          username: "bob",
        },
      ],
    }),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();

  return {
    ...actual,
    getRouteApi: () => ({
      useNavigate: () => vi.fn(),
      useSearch: () => ({}),
    }),
  };
});

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ user: { name: "admin" } }),
}));

vi.mock("./CreateUserDialog", () => ({ CreateUserDialog: () => null }));
vi.mock("./EditUserDialog", () => ({ EditUserDialog: () => null }));
vi.mock("./ChangePasswordDialog", () => ({ ChangePasswordDialog: () => null }));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });

  return { promise, resolve };
}

describe("UsersTab mutation feedback", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("keeps lock feedback attached to each affected user across view changes", async () => {
    const lockAlice = deferred<void>();
    const unlockBob = deferred<void>();

    vi.spyOn(core, "request").mockImplementation((_host, command, request) => {
      const username = (request as { username?: string } | undefined)?.username;
      if (command === "lock_user" && username === "alice") {
        return lockAlice.promise;
      }
      if (command === "unlock_user" && username === "bob") {
        return unlockBob.promise;
      }

      return Promise.resolve(undefined);
    });

    const view = render(<UsersTab viewMode="table" />);

    const lockButton = screen.getByRole("button", { name: "Lock alice" });
    const unlockButton = screen.getByRole("button", { name: "Unlock bob" });

    await act(async () => {
      lockButton.click();
    });

    const lockingButton = screen.getByRole("button", { name: "Locking alice" });
    expect(within(lockingButton).getByRole("progressbar")).toBeInTheDocument();
    expect(unlockButton).toBeEnabled();

    view.rerender(<UsersTab viewMode="card" />);

    expect(
      within(screen.getByRole("button", { name: "Locking alice" })).getByRole(
        "progressbar",
      ),
    ).toBeInTheDocument();

    await act(async () => {
      screen.getByRole("button", { name: "Unlock bob" }).click();
    });

    expect(
      within(screen.getByRole("button", { name: "Unlocking bob" })).getByRole(
        "progressbar",
      ),
    ).toBeInTheDocument();

    await act(async () => {
      lockAlice.resolve();
      await lockAlice.promise;
    });

    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Locking alice" }),
      ).not.toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "Unlocking bob" }),
    ).toBeInTheDocument();

    await act(async () => {
      unlockBob.resolve();
      await unlockBob.promise;
    });

    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "Unlocking bob" }),
      ).not.toBeInTheDocument();
    });
  });
});
