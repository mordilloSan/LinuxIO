import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import FileBrowserHeader from "./FileBrowserHeader";

vi.mock("./IndexerDialog", () => ({ default: () => null }));
vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({ isEnabled: false, reason: "Unavailable" }),
}));
vi.mock("@/hooks/backgroundJobs/useIsIndexing", () => ({
  useIsIndexing: () => false,
}));
vi.mock("@/hooks/backgroundJobs/useBackgroundJobActions", () => ({
  useBackgroundJobActions: () => ({
    openIndexerDialog: vi.fn(),
    startIndexer: vi.fn(),
  }),
}));

const defaultProps = {
  onSwitchView: vi.fn(),
  onToggleHiddenFiles: vi.fn(),
  showHiddenFiles: false,
  showQuickSave: true,
  viewMode: "list" as const,
};

describe("FileBrowserHeader", () => {
  it("disables absent quick-save callbacks and enables supplied handlers", async () => {
    const { rerender, user } = render(<FileBrowserHeader {...defaultProps} />);

    const closeButton = screen.getByRole("button", { name: "Close editor" });
    const saveButton = screen.getByRole("button", { name: "Save changes" });
    expect(closeButton).toBeDisabled();
    expect(saveButton).toBeDisabled();

    const onCloseEditor = vi.fn();
    const onSaveFile = vi.fn().mockResolvedValue(undefined);
    rerender(
      <FileBrowserHeader
        {...defaultProps}
        onCloseEditor={onCloseEditor}
        onSaveFile={onSaveFile}
      />,
    );

    const enabledClose = screen.getByRole("button", { name: "Close editor" });
    const enabledSave = screen.getByRole("button", { name: "Save changes" });
    expect(enabledClose).toBeEnabled();
    expect(enabledSave).toBeEnabled();
    await user.click(enabledClose);
    await user.click(enabledSave);
    expect(onCloseEditor).toHaveBeenCalledOnce();
    expect(onSaveFile).toHaveBeenCalledOnce();
  });

  it("uses the shared view mode toggle", async () => {
    const onSwitchView = vi.fn();
    const { user } = render(
      <FileBrowserHeader
        {...defaultProps}
        onSwitchView={onSwitchView}
        showQuickSave={false}
      />,
    );

    const button = screen.getByRole("button", { name: "Switch to card view" });
    expect(button).toHaveAttribute("aria-label", "Switch to card view");

    await user.hover(button);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Switch to card view",
    );

    await user.click(button);
    expect(onSwitchView).toHaveBeenCalledOnce();
  });
});
