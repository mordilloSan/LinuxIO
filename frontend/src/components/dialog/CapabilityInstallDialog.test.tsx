import { describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen } from "@/test/render";

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
    { stream: "status", text: "Resolving package names" },
    { stream: "stdout", text: "Probing hardware" },
    { stream: "stderr", text: "Driver warning" },
  ] satisfies CapabilityInstallOutputLine[],
  percentage: 42,
  running: true,
  stage: "install_package",
  success: false,
  warning: null,
};

describe("CapabilityInstallDialog", () => {
  it("shows progress and preserves typed installer output", () => {
    render(<CapabilityInstallDialog {...runningProps} />);

    expect(screen.getByText("Installing lm-sensors")).toBeInTheDocument();
    expect(screen.getByText("install package")).toBeInTheDocument();
    expect(screen.getByText("42%")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "Show installation output" }),
    );

    expect(screen.getByText("[status] Resolving package names")).toBeVisible();
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
        output={[{ stream: "stdout", text: "Newest line" }]}
      />,
    );

    expect(scroller?.scrollTop).toBe(240);
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
