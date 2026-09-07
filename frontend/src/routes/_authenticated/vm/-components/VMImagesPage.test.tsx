import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor, within } from "@/test/render";

import VMImagesPage from "./VMImagesPage";

const mocks = vi.hoisted(() => ({
  download: vi.fn(),
  remove: vi.fn(),
  list: vi.fn(),
  template: {
    id: "a".repeat(64),
    imagePresetId: "home-assistant-os",
    label: "Home Assistant OS",
    version: "16.0",
    downloadedAt: "2026-09-07T12:00:00Z",
    sizeBytes: 1024,
    path: "/templates/home-assistant-os/image.qcow2",
    sourceUrl: "https://example.com/haos.qcow2.xz",
  },
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: () => ({ mutate: mocks.remove, isPending: false }),
    linuxio: {
      ...actual.linuxio,
      virt: {
        ...actual.linuxio.virt,
        preflight: () => ({
          queryKey: ["test-preflight"],
          queryFn: async () => ({
            managedPaths: { cloudImages: "/vm-disks", isos: "/isos" },
          }),
        }),
        templates: {
          queryKey: ["test-templates"],
          queryFn: () => mocks.list(),
        },
        template_download: {
          useTaskStreamAction: () => ({
            mutate: mocks.download,
            isPending: false,
          }),
        },
      },
    },
  };
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue({
    path: "/templates",
    templates: [mocks.template],
  });
});

describe("VM template manager", () => {
  it("shows saved versions and placement, and starts explicit downloads", async () => {
    const user = userEvent.setup();
    render(<VMImagesPage />);
    expect(
      await screen.findByText("Version 16.0 · Newest saved"),
    ).toBeInTheDocument();
    expect(screen.getByText(mocks.template.path)).toBeInTheDocument();
    expect(screen.getByText(mocks.template.id)).toBeInTheDocument();
    expect(mocks.download).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", {
        name: "Download update for Home Assistant OS",
      }),
    );
    expect(mocks.download).toHaveBeenCalledWith({
      imagePresetId: "home-assistant-os",
    });
    await user.click(
      screen.getByRole("button", { name: "Download Debian Server" }),
    );
    expect(mocks.download).toHaveBeenCalledWith({
      imagePresetId: "debian-server",
    });
    await user.click(screen.getByRole("button", { name: "Refresh templates" }));
    await waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(2));
  });

  it("requires confirmation before deleting exactly the chosen version", async () => {
    const user = userEvent.setup();
    render(<VMImagesPage />);
    await user.click(
      await screen.findByRole("button", { name: /^Delete Home Assistant/ }),
    );
    expect(mocks.remove).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText(/Existing VMs keep their independent disks/),
    ).toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "Delete template" }),
    );
    expect(mocks.remove).toHaveBeenCalledWith({
      imagePresetId: "home-assistant-os",
      templateId: mocks.template.id,
    });
  });

  it("shows a listing error without suggesting templates are absent", async () => {
    mocks.list.mockRejectedValue(new Error("storage unavailable"));
    render(<VMImagesPage />);
    expect(
      await screen.findByText(
        /Unable to load saved templates: storage unavailable/,
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Not downloaded/)).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Download Home Assistant OS" }),
    ).toBeDisabled();
  });
});
