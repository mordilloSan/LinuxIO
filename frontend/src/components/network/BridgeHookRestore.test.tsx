import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NetworkBridgeHook } from "@/api";
import { render, screen } from "@/test/render";

import BridgeHookRestore from "./BridgeHookRestore";

const mocks = vi.hoisted(() => ({
  restore: vi.fn(),
  useMutation: vi.fn(),
  state: {
    error: null as { message: string } | null,
    isPending: false,
  },
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  return {
    ...actual,
    linuxio: mockMutationHooks(actual.linuxio, mocks.useMutation),
  };
});

mocks.useMutation.mockImplementation(() => ({
  error: mocks.state.error,
  isPending: mocks.state.isPending,
  mutate: mocks.restore,
}));

const missing: NetworkBridgeHook = { restorable: true, state: "missing" };

describe("BridgeHookRestore", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.state = { error: null, isPending: false };
  });

  it("restores the hook for a privileged session", async () => {
    const { user } = render(<BridgeHookRestore hook={missing} />, {
      auth: { privileged: true },
    });

    await user.click(screen.getByRole("button", { name: "Restore" }));

    expect(mocks.restore).toHaveBeenCalledTimes(1);
    expect(mocks.useMutation).toHaveBeenCalledWith(
      { route: "network.restore_bridge_hook" },
      expect.anything(),
    );
  });

  it("offers a restore for a hook that is not executable", () => {
    render(
      <BridgeHookRestore
        hook={{ restorable: true, state: "not_executable" }}
      />,
      { auth: { privileged: true } },
    );

    expect(screen.getByRole("button", { name: "Restore" })).toBeEnabled();
  });

  it.each([
    ["a session without sudo", missing, false],
    ["no package copy", { ...missing, restorable: false }, true],
    ["an installed hook", { restorable: true, state: "installed" }, true],
    ["no hook state", undefined, true],
  ] as const)("offers nothing with %s", (_name, hook, privileged) => {
    render(<BridgeHookRestore hook={hook} />, { auth: { privileged } });

    expect(
      screen.queryByRole("button", { name: "Restore" }),
    ).not.toBeInTheDocument();
  });

  it("holds the button while the restore runs", () => {
    mocks.state.isPending = true;
    render(<BridgeHookRestore hook={missing} />, {
      auth: { privileged: true },
    });

    expect(screen.getByRole("button", { name: "Restoring…" })).toBeDisabled();
  });

  it("keeps a failure in view after the warning has gone", () => {
    mocks.state.error = {
      message:
        "libvirt hook restored, but restarting libvirt failed, so libvirt runs the hook only after its daemon next starts",
    };
    render(
      <BridgeHookRestore hook={{ restorable: true, state: "installed" }} />,
      { auth: { privileged: true } },
    );

    expect(
      screen.getByText("Unable to restore the libvirt hook"),
    ).toBeInTheDocument();
    expect(screen.getByText(/restarting libvirt failed/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Restore" }),
    ).not.toBeInTheDocument();
  });
});
