import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { SMBSettings } from "@/api";
import { renderWithTanStackRouter, screen, within } from "@/test/render";

import SMBSettingsDialog from "./SMBSettingsDialog";

const mocks = vi.hoisted(() => ({
  settings: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
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
      shares: {
        ...actual.linuxio.shares,
        get_smb_settings: {
          queryKey: ["test-smb-settings"],
          queryFn: () => mocks.settings(),
        },
      },
    },
  };
  return {
    ...mocked,
    linuxio: mockMutationHooks(mocked.linuxio, useCallMutation),
  };
});

const saved: SMBSettings = {
  minProtocol: "SMB2",
  encryption: "default",
  macDiscovery: true,
  windowsDiscovery: false,
};

const all = {
  auth: { privileged: true },
  capabilities: {
    avahiAvailable: true,
    sambaServerAvailable: true,
    wsddAvailable: true,
  },
};

const MAC = "Show this server to Macs in Finder";
const WINDOWS = "Show this server to Windows in Network";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.settings.mockResolvedValue(saved);
});

const pick = async (
  user: ReturnType<typeof userEvent.setup>,
  field: string,
  option: string | RegExp,
) => {
  await user.click(await screen.findByRole("combobox", { name: field }));
  await user.click(await screen.findByRole("option", { name: option }));
};

describe("SMBSettingsDialog", () => {
  it("shows the service's settings and saves what was changed", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(<SMBSettingsDialog onClose={vi.fn()} open />, all);
    const dialog = await screen.findByRole("dialog", { name: "SMB service" });
    expect(await within(dialog).findByLabelText(MAC)).toBeChecked();
    expect(within(dialog).getByLabelText(WINDOWS)).not.toBeChecked();
    await pick(user, "Oldest protocol", "SMB3");
    await pick(user, "Encryption", "Required");
    await user.click(within(dialog).getByLabelText(WINDOWS));
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(mocks.mutations["shares.set_smb_settings"]).toHaveBeenCalledWith({
      minProtocol: "SMB3",
      encryption: "required",
      macDiscovery: true,
      windowsDiscovery: true,
    });
  });

  it("says that hiding from Macs restarts the service", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(<SMBSettingsDialog onClose={vi.fn()} open />, all);
    const dialog = await screen.findByRole("dialog", { name: "SMB service" });
    expect(within(dialog).queryByText(/restarts the SMB service/)).toBeNull();
    await user.click(await within(dialog).findByLabelText(MAC));
    expect(
      within(dialog).getByText(/restarts the SMB service/),
    ).toBeInTheDocument();
  });

  it("offers discovery only where its service is installed", async () => {
    renderWithTanStackRouter(<SMBSettingsDialog onClose={vi.fn()} open />, {
      ...all,
      capabilities: {
        avahiAvailable: false,
        sambaServerAvailable: true,
        wsddAvailable: false,
      },
    });
    const dialog = await screen.findByRole("dialog", { name: "SMB service" });
    expect(await within(dialog).findByLabelText(MAC)).toBeDisabled();
    expect(within(dialog).getByLabelText(WINDOWS)).toBeDisabled();
    expect(
      within(dialog).getAllByRole("link", {
        name: "Install it in Settings",
      })[0],
    ).toHaveAttribute("href", "/settings?tab=capabilities");
  });

  // A protocol set by hand that LinuxIO does not offer is shown, and has to
  // be replaced before anything is saved.
  it("shows an older protocol set by hand and asks for one it offers", async () => {
    const user = userEvent.setup();
    mocks.settings.mockResolvedValue({ ...saved, minProtocol: "NT1" });
    renderWithTanStackRouter(<SMBSettingsDialog onClose={vi.fn()} open />, all);
    const dialog = await screen.findByRole("dialog", { name: "SMB service" });
    expect(
      await within(dialog).findByText(/NT1 is older than LinuxIO offers/),
    ).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    await pick(user, "Oldest protocol", "SMB2");
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeEnabled();
  });
});
