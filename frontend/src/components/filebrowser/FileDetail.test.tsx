import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";
import type { FileResource } from "@/types/filebrowser";

import FileDetail from "./FileDetail";

vi.mock("@/hooks/filebrowser/useFileDirectorySize", () => ({
  useFileDirectorySize: () => ({
    error: null,
    isLoading: false,
    isUnavailable: false,
    size: null,
  }),
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
});
