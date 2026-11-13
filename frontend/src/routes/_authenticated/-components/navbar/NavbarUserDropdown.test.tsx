import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import NavbarUserDropdown from "./NavbarUserDropdown";

const mocks = vi.hoisted(() => ({
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
  default: () => ({ triggerPowerOff: vi.fn(), triggerReboot: vi.fn() }),
}));

describe("NavbarUserDropdown", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.useCallMutation.mockReturnValue({ mutate: vi.fn() });
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
});
