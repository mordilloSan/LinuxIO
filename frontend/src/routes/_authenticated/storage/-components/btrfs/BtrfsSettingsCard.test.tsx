import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsSettings } from "@/api";
import { render } from "@/test/render";

import BtrfsSettingsCard from "./BtrfsSettingsCard";

const mocks = vi.hoisted(() => ({
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
  settings: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
  });
  const mocked = {
    ...actual,
    useCallMutation,
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_settings: (request: { uuid: string }) => ({
          queryKey: ["test-btrfs-settings", request.uuid],
          queryFn: () => mocks.settings(request),
        }),
        get_btrfs_maintenance: {
          queryKey: ["test-btrfs-maintenance"],
          queryFn: () =>
            Promise.resolve({
              tasks: [
                {
                  task: "scrub",
                  period: "monthly",
                  periods: ["none", "monthly"],
                  mountpoints: ["/"],
                },
              ],
            }),
        },
      },
    },
  };
  return {
    ...mocked,
    linuxio: mockMutationHooks(mocked.linuxio, useCallMutation),
  };
});

const settings: BtrfsSettings = {
  uuid: "uuid-1",
  quotasEnabled: false,
  compression: { algorithm: "none", force: false, fstabPersisted: true },
};

const route = (name: string) => {
  const mutate = mocks.mutations[`storage.${name}`];
  if (!mutate) throw new Error(`no mutation registered for ${name}`);
  return mutate;
};

const capabilities = { btrfsMaintenanceAvailable: true };

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.settings.mockResolvedValue(settings);
});

describe("BtrfsSettingsCard", () => {
  it("toggles quotas", async () => {
    const user = userEvent.setup();
    render(<BtrfsSettingsCard mountpoint="/" uuid="uuid-1" />, {
      capabilities,
    });

    await user.click(await screen.findByRole("checkbox", { name: "Quotas" }));

    expect(route("btrfs_set_quotas")).toHaveBeenCalledWith({
      uuid: "uuid-1",
      enabled: true,
    });
  });

  it("applies compression with a level and shows fstab persistence", async () => {
    const user = userEvent.setup();
    render(<BtrfsSettingsCard mountpoint="/" uuid="uuid-1" />, {
      capabilities,
    });

    expect(await screen.findByText("Persisted in fstab")).toBeInTheDocument();
    // Filesystem-wide: one control, not one per mount.
    expect(
      screen.getAllByRole("combobox", { name: "Compression" }),
    ).toHaveLength(1);
    expect(screen.queryByRole("spinbutton", { name: "Level" })).toBeNull();
    await user.click(screen.getByRole("combobox", { name: "Compression" }));
    await user.click(screen.getByRole("option", { name: "zstd" }));
    const level = screen.getByRole("spinbutton", { name: "Level" });
    expect(level).toHaveValue(3);
    await user.clear(level);
    await user.type(level, "3");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    expect(route("btrfs_set_compression")).toHaveBeenCalledWith(
      {
        uuid: "uuid-1",
        algorithm: "zstd",
        level: 3,
        force: false,
      },
      expect.anything(),
    );
  });

  it("renders the maintenance timers inside the card", async () => {
    render(<BtrfsSettingsCard mountpoint="/" uuid="uuid-1" />, {
      capabilities,
    });

    const card = await screen.findByRole("region", {
      name: "Scheduled maintenance",
    });
    expect(within(card).getByText("Scrub")).toBeInTheDocument();
  });
});
