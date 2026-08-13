import { describe, expect, it, vi } from "vitest";

import type { DockerContainerAutoUpdateState } from "@/api";
import { render, screen } from "@/test/render";

import ContainerAutoUpdateDialog from "./ContainerAutoUpdateDialog";
import type { ContainerAutoUpdateController } from "./useContainerAutoUpdateState";

const blockedReason =
  "Container is not running; start it before enabling automatic updates.";

const createController = (): ContainerAutoUpdateController => {
  const state: DockerContainerAutoUpdateState = {
    available: true,
    containers: [],
    missing_container_names: [],
    options: {
      cleanup: false,
      container_names: ["stopped"],
      enabled: true,
      mode: "update",
      time: "04:00",
    },
    timer_active: false,
    timer_enabled: true,
  };

  return {
    disabled: false,
    isPending: false,
    isSaving: false,
    pendingNames: new Set(),
    queryError: undefined,
    reason: undefined,
    saveOptions: vi.fn(),
    selectedNames: new Set(["stopped"]),
    state,
    targetEligibility: new Map([
      ["stopped", { mutationAllowed: false, mutationReason: blockedReason }],
    ]),
    toggleContainer: vi.fn(),
  };
};

const renderDialog = (autoUpdate = createController()) =>
  render(
    <ContainerAutoUpdateDialog
      autoUpdate={autoUpdate}
      dockerUpdatesEnabled
      onClose={vi.fn()}
      open
    />,
  );

describe("ContainerAutoUpdateDialog eligibility", () => {
  it("keeps an ineligible selected target removable and blocks update-mode saves", async () => {
    const { user } = renderDialog();

    expect(
      screen.getByText("Selected containers cannot be updated automatically"),
    ).toBeInTheDocument();
    const remove = screen.getByRole("button", { name: "Remove stopped" });
    expect(remove).toBeEnabled();

    const time = screen.getByDisplayValue("04:00");
    await user.clear(time);
    await user.type(time, "05:00");

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByText(blockedReason)).toBeInTheDocument();

    await user.click(remove);
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("allows saving the same selection in check-only mode", async () => {
    const autoUpdate = createController();
    const { user } = renderDialog(autoUpdate);

    await user.click(screen.getByRole("combobox"));
    await user.click(screen.getByRole("option", { name: "Check only" }));

    expect(
      screen.queryByText("Selected containers cannot be updated automatically"),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(autoUpdate.saveOptions).toHaveBeenCalledWith(
      expect.objectContaining({
        container_names: ["stopped"],
        mode: "check_only",
      }),
    );
  });
});
