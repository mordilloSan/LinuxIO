import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { HeaderActionsProps } from "@/components/ui/HeaderActions";
import { render, screen } from "@/test/render";

import FileBrowserHeader from "./FileBrowserHeader";

const headerActionsRender = vi.hoisted(() => vi.fn());

vi.mock("./IndexerDialog", () => ({ default: () => null }));
vi.mock("@/components/ui/HeaderActions", () => ({
  default: (slots: HeaderActionsProps) => {
    headerActionsRender();
    return (
      <>
        {slots.options}
        {slots.view}
      </>
    );
  },
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

const SearchableMobileHeader = () => {
  const [searchQuery, setSearchQuery] = useState("");

  return (
    <FileBrowserHeader
      {...defaultProps}
      breadcrumbs={<div>Home</div>}
      onSearchChange={setSearchQuery}
      searchQuery={searchQuery}
      showQuickSave={false}
    />
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

  it("keeps browsing actions stable for breadcrumb-only renders", () => {
    headerActionsRender.mockClear();
    const browsingProps = {
      ...defaultProps,
      showQuickSave: false,
    };
    const { rerender } = render(
      <FileBrowserHeader {...browsingProps} breadcrumbs={<div>Root</div>} />,
    );

    expect(headerActionsRender).toHaveBeenCalledTimes(1);

    rerender(
      <FileBrowserHeader
        {...browsingProps}
        breadcrumbs={<div>Next directory</div>}
        searchQuery="notes"
      />,
    );

    expect(headerActionsRender).toHaveBeenCalledTimes(1);
    expect(
      screen.getByRole("textbox", { name: "Search files and folders..." }),
    ).toHaveValue("notes");

    rerender(
      <FileBrowserHeader
        {...browsingProps}
        breadcrumbs={<div>Next directory</div>}
      />,
    );

    expect(headerActionsRender).toHaveBeenCalledTimes(1);

    rerender(
      <FileBrowserHeader
        {...browsingProps}
        breadcrumbs={<div>Next directory</div>}
        viewMode="card"
      />,
    );

    expect(headerActionsRender).toHaveBeenCalledTimes(2);
    expect(
      screen.getByRole("button", { name: "Switch to list view" }),
    ).toBeVisible();

    rerender(
      <FileBrowserHeader
        {...browsingProps}
        breadcrumbs={<div>Next directory</div>}
        showHiddenFiles
        viewMode="card"
      />,
    );

    expect(headerActionsRender).toHaveBeenCalledTimes(3);
    expect(
      screen.getByRole("button", { name: "Hide hidden files" }),
    ).toBeVisible();
  });

  it("centers the search with the tab strip's columns on desktop", () => {
    const { container } = render(
      <FileBrowserHeader
        {...defaultProps}
        breadcrumbs={<div>Home</div>}
        showQuickSave={false}
      />,
    );

    // Mirrors .tab-selector in tab-selector.css — the search must sit at the
    // same screen-centered spot here as on tab routes.
    expect(container.querySelector(".file-browser-header")).toHaveStyle({
      display: "grid",
      gridTemplateColumns:
        "minmax(0, 1fr) clamp(140px, 40vw, 400px) minmax(0, 1fr)",
    });
    expect(
      screen.getByRole("textbox", { name: "Search files and folders..." }),
    ).toBeVisible();
  });

  it("opens and focuses search from the mobile actions menu", async () => {
    mockMobileViewport();
    const { container, user } = render(<SearchableMobileHeader />);

    expect(container.querySelector(".file-browser-header")).toHaveStyle({
      display: "grid",
      gridTemplateColumns: "minmax(0, 1fr) auto",
    });
    expect(
      container.querySelector(".file-browser-header__breadcrumbs"),
    ).toHaveStyle({ gridColumn: "1" });
    expect(
      container.querySelector(".file-browser-header__actions"),
    ).toHaveStyle({
      gridColumn: "2",
      justifySelf: "end",
    });
    expect(
      screen.queryByRole("textbox", { name: "Search files and folders..." }),
    ).not.toBeInTheDocument();

    const actionsTrigger = screen.getByRole("button", { name: "Actions" });
    await user.click(actionsTrigger);
    const mobileActions = document.querySelector(".app-mobile-actions-menu");
    expect(mobileActions).toContainElement(
      screen.getByRole("button", { name: "Search" }),
    );
    expect(mobileActions).toContainElement(
      screen.getByRole("button", { name: "Switch to card view" }),
    );
    expect(mobileActions).toContainElement(
      screen.getByRole("button", { name: "Show hidden files" }),
    );
    expect(mobileActions).toContainElement(
      screen.getByRole("button", { name: "Index filesystem" }),
    );
    await user.click(screen.getByRole("button", { name: "Search" }));

    const search = await screen.findByRole("textbox", {
      name: "Search files and folders...",
    });
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(actionsTrigger).toHaveAttribute("aria-expanded", "false");
    expect(search).toHaveFocus();
    expect(
      document.querySelector(".file-browser-header__mobile-search"),
    ).toContainElement(search);

    await user.type(search, "notes");
    expect(actionsTrigger).toHaveClass("app-icon-btn--primary");
    await user.keyboard("{Escape}");

    expect(search).toHaveValue("");
    expect(search).toHaveFocus();

    await user.keyboard("{Escape}");

    expect(
      screen.queryByRole("textbox", { name: "Search files and folders..." }),
    ).not.toBeInTheDocument();
    expect(actionsTrigger).toHaveFocus();
  });
});
