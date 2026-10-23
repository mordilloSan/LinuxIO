import { beforeEach, describe, expect, it, vi } from "vitest";

import type { VirtualMachine } from "@/api";
import { renderWithTanStackRouter, screen } from "@/test/render";

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
  it("toggles autostart", async () => {
    const { user } = renderWithTanStackRouter(<VMDetailsPanel vm={vm} />);
    await user.click(
      await screen.findByRole("checkbox", { name: "Autostart" }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({ autostart: true, name: "HA" });
  });

  it("shows the firmware without an inline select", async () => {
    renderWithTanStackRouter(<VMDetailsPanel vm={vm} />);
    expect(await screen.findByText("BIOS (legacy)")).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });
});
