import { afterEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import FileBrowserHeader from "./FileBrowserHeader";

vi.mock("./IndexerDialog", () => ({ default: () => null }));
vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({ isEnabled: false, reason: "Unavailable" }),
}));
vi.mock("@/hooks/backgroundTasks/useIsIndexing", () => ({
  useIsIndexing: () => false,
}));
vi.mock("@/hooks/backgroundTasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({
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

const mockMobileViewport = () => {
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query) =>
      ({
        addEventListener: vi.fn(),
        matches: true,
        media: query,
        onchange: null,
        removeEventListener: vi.fn(),
      }) as unknown as MediaQueryList,
  );
};

describe("FileBrowserHeader", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

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

  it("centers the visible search between equal mobile side columns", () => {
    mockMobileViewport();
    const { container } = render(
      <FileBrowserHeader
        {...defaultProps}
        breadcrumbs={<div>Home</div>}
        showQuickSave={false}
      />,
    );

    expect(container.querySelector(".file-browser-header")).toHaveStyle({
      display: "grid",
      gridTemplateColumns:
        "minmax(0, 1fr) clamp(260px, 40vw, 420px) minmax(0, 1fr)",
    });
    expect(
      container.querySelector(".file-browser-header__breadcrumbs"),
    ).toHaveStyle({ gridColumn: "1" });
    expect(container.querySelector(".file-browser-header__search")).toHaveStyle(
      { gridColumn: "2" },
    );
    expect(container.querySelector(".header-right")).toHaveStyle({
      gridColumn: "3",
      justifySelf: "end",
    });
    expect(screen.getByRole("textbox", { name: "Search..." })).toBeVisible();
  });
});
