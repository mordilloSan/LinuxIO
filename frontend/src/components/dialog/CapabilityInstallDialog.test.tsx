import { describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen, waitFor } from "@/test/render";

import CapabilityInstallDialog, {
  type CapabilityInstallOutputLine,
} from "./CapabilityInstallDialog";

const runningProps = {
  capabilityLabel: "lm-sensors",
  error: null,
  message: "Installing packages",
  onClose: vi.fn(),
  open: true,
  output: [
    { id: 0, stream: "status", text: "Resolving package names" },
    { id: 1, stream: "stdout", text: "Probing hardware" },
    { id: 2, stream: "stderr", text: "Driver warning" },
  ] satisfies CapabilityInstallOutputLine[],
  percentage: 42,
  running: true,
  stage: "install_package",
  success: false,
  warning: null,
};

describe("CapabilityInstallDialog", () => {
  it("shows progress and preserves typed installer output", async () => {
    render(<CapabilityInstallDialog {...runningProps} />);

    expect(screen.getByText("Installing lm-sensors")).toBeInTheDocument();
    expect(screen.getByText("install package")).toBeInTheDocument();
    expect(screen.getByText("42%")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "Show installation output" }),
    );

    await waitFor(() =>
      expect(
        screen.getByText("[status] Resolving package names"),
      ).toBeVisible(),
    );
    expect(screen.getByText("Probing hardware")).toBeVisible();
    expect(screen.getByText("Driver warning")).toHaveClass(
      "capability-install-dialog__output-line--stderr",
    );
  });

  it("pins an expanded output view to the latest record", () => {
    const { rerender } = render(
      <CapabilityInstallDialog {...runningProps} output={[]} />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Show installation output" }),
    );

    const scroller = document.getElementById("capability-install-output");
    expect(scroller).not.toBeNull();
    Object.defineProperty(scroller, "scrollHeight", {
      configurable: true,
      value: 240,
    });

    rerender(
      <CapabilityInstallDialog
        {...runningProps}
        output={[{ id: 0, stream: "stdout", text: "Newest line" }]}
      />,
    );

    expect(scroller?.scrollTop).toBe(240);
  });

  it("preserves retained output row identity as the oldest record is removed", () => {
    const initialOutput = Array.from({ length: 3 }, (_, id) => ({
      id,
      stream: "stdout" as const,
      text: `line ${id}`,
    }));
    const { rerender } = render(
      <CapabilityInstallDialog {...runningProps} output={initialOutput} />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Show installation output" }),
    );
    const retainedRow = screen.getByText("line 1");

    rerender(
      <CapabilityInstallDialog
        {...runningProps}
        output={[
          { id: 1, stream: "stdout", text: "line 1" },
          { id: 2, stream: "stdout", text: "line 2" },
          { id: 3, stream: "stdout", text: "line 3" },
        ]}
      />,
    );

    expect(screen.getByText("line 1")).toBe(retainedRow);
  });

  it("allows a running install to continue in the background", () => {
    const onClose = vi.fn();
    render(<CapabilityInstallDialog {...runningProps} onClose={onClose} />);

    fireEvent.click(screen.getByRole("button", { name: "Run in background" }));

    expect(onClose).toHaveBeenCalledOnce();
  });

  it("renders recovered-history, success, and failure states honestly", () => {
    const { rerender } = render(
      <CapabilityInstallDialog
        {...runningProps}
        output={[]}
        outputHistoryIncomplete
      />,
    );

    expect(
      screen.getByText(/Earlier records may be unavailable/),
    ).toBeInTheDocument();

    rerender(
      <CapabilityInstallDialog
        {...runningProps}
        message="Finished"
        output={[]}
        percentage={100}
        running={false}
        success
      />,
    );
    expect(screen.getByText("✓ lm-sensors installed")).toBeInTheDocument();

    rerender(
      <CapabilityInstallDialog
        {...runningProps}
        output={[]}
        percentage={100}
        running={false}
        warning="Installed, but sensors is still unavailable"
      />,
    );
    expect(
      screen.getByText("Installed, but sensors is still unavailable"),
    ).toBeInTheDocument();

    rerender(
      <CapabilityInstallDialog
        {...runningProps}
        error="sensors-detect failed"
        output={[]}
        percentage={null}
        running={false}
      />,
    );
    expect(screen.getByText("sensors-detect failed")).toBeInTheDocument();
  });
});
