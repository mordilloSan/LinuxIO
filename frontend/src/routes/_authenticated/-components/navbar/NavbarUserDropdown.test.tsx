import { beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError } from "@/api/errors";
import { render, screen } from "@/test/render";

import NavbarUserDropdown from "./NavbarUserDropdown";

const mocks = vi.hoisted(() => ({
  settlePowerAction: vi.fn(),
  triggerPowerOff: vi.fn(),
  triggerReboot: vi.fn(),
  useCallMutation: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  return {
    ...actual,
    linuxio: mockMutationHooks(actual.linuxio, mocks.useCallMutation),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();
  return { ...actual, useNavigate: () => vi.fn() };
});

vi.mock("@/hooks/usePowerAction", () => ({
  default: () => ({
    triggerPowerOff: mocks.triggerPowerOff,
    triggerReboot: mocks.triggerReboot,
  }),
}));

describe("NavbarUserDropdown", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.useCallMutation.mockReturnValue({ mutate: vi.fn() });
    mocks.triggerReboot.mockReturnValue(mocks.settlePowerAction);
    mocks.triggerPowerOff.mockReturnValue(mocks.settlePowerAction);
  });

  it("offers reboot and power down to a session with sudo", async () => {
    const { user } = render(<NavbarUserDropdown />, {
      auth: { privileged: true },
    });

    await user.click(screen.getByRole("button", { name: "Account" }));

    expect(screen.getByText("Reboot")).toBeInTheDocument();
    expect(screen.getByText("Power Down")).toBeInTheDocument();
    expect(screen.getByText("Sign out")).toBeInTheDocument();
  });

  it("offers only sign out to a session without sudo", async () => {
    const { user } = render(<NavbarUserDropdown />, {
      auth: { privileged: false },
    });

    await user.click(screen.getByRole("button", { name: "Account" }));

    expect(screen.getByText("Sign out")).toBeInTheDocument();
    expect(screen.queryByText("Reboot")).not.toBeInTheDocument();
    expect(screen.queryByText("Power Down")).not.toBeInTheDocument();
  });

  it("asks for confirmation, shows the overlay, then sends the reboot once", async () => {
    const mutate = vi.fn();
    mocks.useCallMutation.mockReturnValue({ mutate });
    const { user } = render(<NavbarUserDropdown />, {
      auth: { privileged: true },
    });

    await user.click(screen.getByRole("button", { name: "Account" }));
    await user.click(screen.getByText("Reboot"));
    expect(mocks.triggerReboot).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Reboot" }));

    expect(mocks.triggerReboot).toHaveBeenCalledTimes(1);
    expect(mutate).toHaveBeenCalledTimes(1);
  });

  it("hands the answer of each power route to the overlay it started", async () => {
    const { user } = render(<NavbarUserDropdown />, {
      auth: { privileged: true },
    });
    const configs = () =>
      Object.fromEntries(
        mocks.useCallMutation.mock.calls.map(([endpoint, config]) => [
          endpoint.route,
          config,
        ]),
      );
    const refusal = new LinuxIOError("access denied", 403);

    await user.click(screen.getByRole("button", { name: "Account" }));
    await user.click(screen.getByText("Reboot"));
    await user.click(screen.getByRole("button", { name: "Reboot" }));
    configs()["control.reboot"].error(refusal);
    expect(mocks.settlePowerAction).toHaveBeenLastCalledWith(refusal);

    await user.click(screen.getByRole("button", { name: "Account" }));
    await user.click(screen.getByText("Power Down"));
    await user.click(screen.getByRole("button", { name: "Power Down" }));
    configs()["control.power_off"].error(refusal);
    expect(mocks.settlePowerAction).toHaveBeenLastCalledWith(refusal);
    configs()["control.power_off"].success();
    expect(mocks.settlePowerAction).toHaveBeenLastCalledWith();
  });
});
