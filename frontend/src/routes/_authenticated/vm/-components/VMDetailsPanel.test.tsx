import { beforeEach, describe, expect, it, vi } from "vitest";

import type { VirtualMachine } from "@/api";
import { renderWithTanStackRouter, screen } from "@/test/render";

import VMDetailsPanel from "./VMDetailsPanel";

const mocks = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = () => ({ mutate: mocks.mutate, isPending: false });
  return {
    ...actual,
    linuxio: mockMutationHooks(actual.linuxio, useCallMutation),
    useCallMutation,
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

  it("lists monitoring, disks and networks", async () => {
    renderWithTanStackRouter(
      <VMDetailsPanel
        vm={{
          ...vm,
          disks: [
            {
              device: "disk",
              owned: true,
              path: "/var/lib/libvirt/images/linuxio-HA.qcow2",
              sizeGB: 32,
              target: "vda",
            },
          ],
          nics: [{ mac: "52:54:00:00:00:01", network: "br0" }],
        }}
      />,
    );
    expect(await screen.findByText("Monitoring")).toBeInTheDocument();
    expect(screen.getByText(/managed · 32 GB/)).toBeInTheDocument();
    expect(
      screen.getByText("No IP lease yet · 52:54:00:00:00:01"),
    ).toBeInTheDocument();
  });
});
