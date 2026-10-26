import { within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";
import { readSigninNotice } from "@/utils/signinNotice";

import AppUpdateBanner from "./AppUpdateBanner";

const mocks = vi.hoisted(() => ({
  muxStatus: "open" as "open" | "error",
  resetUpdate: vi.fn(),
  startUpdate: vi.fn(),
  updater: {} as Record<string, unknown>,
}));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  useStreamMux: () => ({
    status: mocks.muxStatus,
    isOpen: mocks.muxStatus === "open",
    getStream: () => null,
  }),
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
    ...mocks.updater,
  }),
}));

const updateInfo = {
  available: true,
  current_version: "1.0.0",
  latest_version: "1.1.0",
};

const finishedUpdate = (success: boolean) => ({
  error: success ? null : "Update failed (exit code 1)",
  output: ["Step 5/5: Installing systemd", "Installation complete"],
  phase: success ? "done" : "failed",
  progress: 100,
  status: success ? "Update complete" : "Update failed",
  targetVersion: "1.1.0",
  updateComplete: true,
  updateSuccess: success,
});

describe("AppUpdateBanner", () => {
  beforeEach(() => {
    mocks.muxStatus = "open";
    mocks.updater = {};
    mocks.resetUpdate.mockReset();
    mocks.startUpdate.mockReset();
    sessionStorage.clear();
    // jsdom has no scrollIntoView; the dialog scrolls its opened output.
    Element.prototype.scrollIntoView = vi.fn();
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
        screen.getByText(
          "The service will restart and you will need to sign in again.",
        ),
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

  it("shows the installer output and signs out to a fresh page after a successful update", async () => {
    mocks.updater = finishedUpdate(true);
    const signOut = vi.fn().mockResolvedValue(undefined);
    const { user } = render(
      <AppUpdateBanner onDismiss={vi.fn()} updateInfo={updateInfo} />,
      { auth: { signOut } },
    );

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent(
      "LinuxIO has been updated to 1.1.0. The service restarted, so you need to sign in again.",
    );
    // The output is readable without a click once the update has finished,
    // and nothing leaves the page until the user does.
    expect(
      within(dialog).getByText("Installation complete"),
    ).toBeInTheDocument();
    expect(
      within(dialog).queryByRole("button", { name: "Close" }),
    ).not.toBeInTheDocument();

    await user.click(
      within(dialog).getByRole("button", { name: "Sign in again" }),
    );
    expect(readSigninNotice()).toBe("updated");
    expect(signOut).toHaveBeenCalledTimes(1);
    expect(mocks.resetUpdate).not.toHaveBeenCalled();
  });

  it("closes a failed update in place while the session is still alive", async () => {
    mocks.updater = finishedUpdate(false);
    const signOut = vi.fn();
    const { user } = render(
      <AppUpdateBanner onDismiss={vi.fn()} updateInfo={updateInfo} />,
      { auth: { signOut } },
    );

    await user.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Close",
      }),
    );
    expect(mocks.resetUpdate).toHaveBeenCalledTimes(1);
    expect(signOut).not.toHaveBeenCalled();
  });

  it("offers a fresh sign-in when the update failed after the service restarted", async () => {
    mocks.muxStatus = "error";
    mocks.updater = finishedUpdate(false);
    const signOut = vi.fn().mockResolvedValue(undefined);
    const { user } = render(
      <AppUpdateBanner onDismiss={vi.fn()} updateInfo={updateInfo} />,
      { auth: { signOut } },
    );

    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).queryByRole("button", { name: "Close" }),
    ).not.toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "Sign in again" }),
    );
    expect(readSigninNotice()).toBe("updated");
    expect(signOut).toHaveBeenCalledTimes(1);
    expect(mocks.resetUpdate).not.toHaveBeenCalled();
  });
});
