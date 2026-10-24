import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

const mocks = vi.hoisted(() => ({
  call: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("sonner", () => ({
  toast: { error: mocks.toastError, success: mocks.toastSuccess },
}));
vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: mocks.call,
}));

import LoggingSettingsSection from "./LoggingSettingsSection";

describe("LoggingSettingsSection", () => {
  let enabled: boolean;

  beforeEach(() => {
    enabled = false;
    mocks.call.mockReset();
    mocks.toastSuccess.mockReset();
    mocks.toastError.mockReset();
    mocks.call.mockImplementation(
      async (route: string, request?: { enabled: boolean }) => {
        if (route === "logging.set_debug" && request) enabled = request.enabled;
        return { enabled };
      },
    );
  });

  it("flips debug logging through set_debug and reflects the new state", async () => {
    const { user } = render(<LoggingSettingsSection />);

    const toggle = await screen.findByRole("checkbox", {
      name: "Debug logging",
    });
    await waitFor(() => expect(toggle).toBeEnabled());
    expect(toggle).not.toBeChecked();
    expect(
      screen.getByText("Writes debug lines to the journal until switched off"),
    ).toBeInTheDocument();

    await user.click(toggle);

    await waitFor(() =>
      expect(mocks.call).toHaveBeenCalledWith("logging.set_debug", {
        enabled: true,
      }),
    );
    await waitFor(() =>
      expect(mocks.toastSuccess).toHaveBeenCalledWith("Debug logging enabled"),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("checkbox", { name: "Debug logging" }),
      ).toBeChecked(),
    );
  });

  it("shows the read error instead of a switch with an unknown state", async () => {
    mocks.call.mockRejectedValue(new Error("journal unreachable"));
    render(<LoggingSettingsSection />);

    expect(await screen.findByText("journal unreachable")).toBeInTheDocument();
    expect(
      screen.queryByRole("checkbox", { name: "Debug logging" }),
    ).not.toBeInTheDocument();
  });

  it("re-reads the state when set_debug fails after the switch already changed", async () => {
    mocks.call.mockImplementation(async (route: string) => {
      if (route === "logging.set_debug") {
        enabled = true;
        throw new Error("reload daemons failed");
      }
      return { enabled };
    });
    const { user } = render(<LoggingSettingsSection />);

    const toggle = await screen.findByRole("checkbox", {
      name: "Debug logging",
    });
    await waitFor(() => expect(toggle).toBeEnabled());
    await user.click(toggle);

    await waitFor(() => expect(mocks.toastError).toHaveBeenCalled());
    await waitFor(() =>
      expect(
        screen.getByRole("checkbox", { name: "Debug logging" }),
      ).toBeChecked(),
    );
  });
});
