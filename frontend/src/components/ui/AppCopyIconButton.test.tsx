import { act, fireEvent, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import AppCopyIconButton from "@/components/ui/AppCopyIconButton";
import { render, screen } from "@/test/render";

describe("AppCopyIconButton", () => {
  it("shows Copied once the clipboard has the text, then goes back", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      render(<AppCopyIconButton getText={() => "a line"} label="Copy logs" />);
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      const writeText = vi.spyOn(navigator.clipboard, "writeText");
      const button = screen.getByRole("button", { name: "Copy logs" });
      const copied = "app-copy-icon-button--copied";
      expect(button).not.toHaveClass(copied);

      await user.click(button);
      expect(writeText).toHaveBeenCalledWith("a line");
      await waitFor(() => expect(button).toHaveClass(copied));
      // The name stays; the result is read from the status line.
      expect(button).toHaveAccessibleName("Copy logs");
      expect(screen.getByRole("status")).toHaveTextContent("Copied");

      act(() => {
        vi.advanceTimersByTime(2000);
      });
      expect(button).not.toHaveClass(copied);
      expect(screen.getByRole("status")).toBeEmptyDOMElement();
    } finally {
      vi.useRealTimers();
    }
  });

  it("shows Copy failed when the Clipboard API is unavailable", async () => {
    render(<AppCopyIconButton getText={() => "a line"} label="Copy logs" />);
    // Removed after render: render sets userEvent up, which puts a clipboard
    // stub on the window.
    const original = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      configurable: true,
    });
    try {
      const button = screen.getByRole("button", { name: "Copy logs" });
      fireEvent.click(button);
      await waitFor(() =>
        expect(button).toHaveClass("app-copy-icon-button--failed"),
      );
      expect(screen.getByRole("status")).toHaveTextContent("Copy failed");
    } finally {
      if (original) Object.defineProperty(navigator, "clipboard", original);
      else delete (navigator as { clipboard?: unknown }).clipboard;
    }
  });

  it("says nothing when there is no text to copy", async () => {
    const { user } = render(
      <AppCopyIconButton getText={() => ""} label="Copy logs" />,
    );
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    await user.click(screen.getByRole("button", { name: "Copy logs" }));
    expect(writeText).not.toHaveBeenCalled();
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
  });
});
