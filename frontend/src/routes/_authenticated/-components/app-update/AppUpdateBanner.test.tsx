import { within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import AppUpdateBanner from "./AppUpdateBanner";

const mocks = vi.hoisted(() => ({
  resetUpdate: vi.fn(),
  startUpdate: vi.fn(),
}));

vi.mock("@/hooks/useAppUpdater", () => ({
  useAppUpdateInProgress: () => false,
  useAppUpdater: () => ({
    error: null,
    output: [],
    phase: "idle",
    progress: 0,
    resetUpdate: mocks.resetUpdate,
    startUpdate: mocks.startUpdate,
    status: "",
    targetVersion: null,
    updateComplete: false,
    updateSuccess: false,
  }),
}));

const updateInfo = {
  available: true,
  current_version: "1.0.0",
  latest_version: "1.1.0",
};

describe("AppUpdateBanner", () => {
  beforeEach(() => {
    mocks.resetUpdate.mockReset();
    mocks.startUpdate.mockReset();
  });

  it("requires confirmation before starting the exact update", async () => {
    const { user } = render(
      <AppUpdateBanner onDismiss={vi.fn()} updateInfo={updateInfo} />,
    );

    await user.click(screen.getByRole("button", { name: "Update Now" }));
    expect(mocks.startUpdate).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toHaveTextContent(
      "Update LinuxIO from 1.0.0 to 1.1.0?",
    );
    // The dialog fades in from opacity 0; wait for the first animation
    // frame instead of racing it.
    await waitFor(() =>
      expect(
        screen.getByText("The service will restart automatically."),
      ).toBeVisible(),
    );

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(mocks.startUpdate).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );

    await user.click(screen.getByRole("button", { name: "Update Now" }));
    await user.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Update",
      }),
    );
    expect(mocks.startUpdate).toHaveBeenCalledWith("1.1.0");
  });
});
