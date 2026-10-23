import { beforeEach, describe, expect, it, vi } from "vitest";

import type { VirtualMachine } from "@/api";
import { renderWithTanStackRouter, screen, within } from "@/test/render";

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

describe("VMDetailsPanel", () => {
  it("edits a shut-off VM in a dialog", async () => {
    const { user } = renderWithTanStackRouter(<VMDetailsPanel vm={vm} />);
    expect(
      screen.queryByRole("combobox", { name: "Firmware" }),
    ).not.toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog");
    const name = within(dialog).getByLabelText(/^name/i);
    await user.clear(name);
    await user.type(name, "HA2");
    await user.click(screen.getByRole("combobox", { name: "Firmware" }));
    await user.click(screen.getByRole("option", { name: "UEFI" }));
    await user.click(
      screen.getByRole("combobox", { name: "Operating system" }),
    );
    await user.click(screen.getByRole("option", { name: "Ubuntu" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(mocks.mutate).toHaveBeenCalledWith({
      firmware: "uefi",
      memoryMB: 4096,
      name: "HA",
      newName: "HA2",
      os: "ubuntu",
      vcpus: 2,
    });
  });

  it("toggles autostart", async () => {
    const { user } = renderWithTanStackRouter(<VMDetailsPanel vm={vm} />);
    await user.click(
      await screen.findByRole("checkbox", { name: "Autostart" }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({ autostart: true, name: "HA" });
  });

  it("locks editing while the VM runs", async () => {
    renderWithTanStackRouter(
      <VMDetailsPanel vm={{ ...vm, state: "running" }} />,
    );
    expect(await screen.findByRole("button", { name: "Edit" })).toBeDisabled();
  });
});
