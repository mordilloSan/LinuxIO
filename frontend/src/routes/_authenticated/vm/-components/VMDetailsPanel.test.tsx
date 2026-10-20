import { beforeEach, describe, expect, it, vi } from "vitest";

import type { VirtualMachine } from "@/api";
import { render, screen } from "@/test/render";

import VMDetailsPanel from "./VMDetailsPanel";

const mocks = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: () => ({ mutate: mocks.mutate, isPending: false }),
  };
});

const vm: VirtualMachine = {
  autostart: false,
  diskGB: 32,
  firmware: "bios",
  hasGraphics: true,
  memoryMB: 4096,
  name: "HA",
  ownedDisks: [],
  state: "shutoff",
  vcpus: 2,
};

beforeEach(() => mocks.mutate.mockReset());

describe("VMDetailsPanel firmware", () => {
  it("switches a shut-off VM to UEFI", async () => {
    const { user } = render(<VMDetailsPanel vm={vm} />);
    await user.click(screen.getByRole("combobox", { name: "Firmware" }));
    await user.click(screen.getByRole("option", { name: "UEFI" }));
    expect(mocks.mutate).toHaveBeenCalledWith({
      firmware: "uefi",
      name: "HA",
    });
  });

  it("toggles autostart", async () => {
    const { user } = render(<VMDetailsPanel vm={vm} />);
    await user.click(screen.getByRole("checkbox", { name: "Autostart" }));
    expect(mocks.mutate).toHaveBeenCalledWith({ autostart: true, name: "HA" });
  });

  it("locks the firmware while the VM runs", () => {
    render(<VMDetailsPanel vm={{ ...vm, state: "running" }} />);
    expect(screen.getByRole("combobox", { name: "Firmware" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(
      screen.getByText("Shut off the VM to change its firmware"),
    ).toBeInTheDocument();
  });
});
