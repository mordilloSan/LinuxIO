import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";
import type { FileResource } from "@/types/filebrowser";

import FileDetail from "./FileDetail";

const directorySizeState = vi.hoisted(() => ({
  error: null as Error | null,
  fileCount: 12,
  folderCount: 3,
  isLoading: false,
  isUnavailable: false,
  size: 1024,
}));

vi.mock("@/hooks/filebrowser/useFileDirectorySize", () => ({
  useFileDirectorySize: () => directorySizeState,
}));

const resource = (overrides: Partial<FileResource> = {}): FileResource => ({
  canOpenAsText: true,
  isRegularFile: true,
  modified: "2026-08-29T00:00:00Z",
  name: "notes",
  path: "/srv/notes",
  size: 5,
  type: "file",
  ...overrides,
});

describe("FileDetail", () => {
  beforeEach(() => {
    directorySizeState.error = null;
    directorySizeState.isLoading = false;
    directorySizeState.isUnavailable = false;
    directorySizeState.size = 1024;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("enables Open text file only when the backend grants eligibility", async () => {
    vi.useFakeTimers();
    const { rerender } = render(
      <FileDetail
        onDownload={vi.fn()}
        onEdit={vi.fn()}
        resource={resource()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Open text file" }),
    ).toBeVisible();

    rerender(
      <FileDetail
        onDownload={vi.fn()}
        onEdit={vi.fn()}
        resource={resource({ canOpenAsText: false })}
      />,
    );
    const button = screen.getByRole("button", { name: "Open text file" });
    expect(button).toBeDisabled();

    fireEvent.mouseEnter(button.parentElement as HTMLElement);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "This file isn't plain text",
    );
  });

  it("shows indexed size and recursive counts for a directory", () => {
    render(
      <FileDetail
        onDownload={vi.fn()}
        resource={resource({
          canOpenAsText: false,
          isRegularFile: false,
          name: "projects",
          path: "/srv/projects",
          size: 0,
          type: "directory",
        })}
      />,
    );

    expect(screen.getByText("Total size")).toBeVisible();
    expect(screen.getByText("1 KB")).toBeVisible();
    expect(screen.getByText("Files")).toBeVisible();
    expect(screen.getByText("12")).toBeVisible();
    expect(screen.getByText("Folders")).toBeVisible();
    expect(screen.getByText("3")).toBeVisible();
  });

  it("marks cached directory sizes unavailable when the indexer is offline", () => {
    directorySizeState.error = new Error("Indexer API unavailable");
    directorySizeState.isUnavailable = true;

    render(
      <FileDetail
        onDownload={vi.fn()}
        resource={resource({
          isRegularFile: false,
          name: "projects",
          path: "/srv/projects",
          size: 0,
          type: "directory",
        })}
      />,
    );

    expect(screen.getByText("Unavailable")).toBeVisible();
    expect(screen.getByText("Indexer API unavailable")).toBeVisible();
  });
});
