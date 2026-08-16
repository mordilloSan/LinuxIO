import { describe, expect, it, vi } from "vitest";

import type { DockerContainerAutoUpdateState } from "@/api";
import { render, screen } from "@/test/render";

import DockerAutoUpdateSettingsSection from "./DockerAutoUpdateSettingsSection";
import type { DockerAutoUpdateController } from "./useDockerAutoUpdateState";

const blockedReason =
  "Container is not running; start it before enabling automatic updates.";

const createController = (): DockerAutoUpdateController => {
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
    isPending: false,
    isSaving: false,
    queryError: undefined,
    saveOptions: vi.fn(),
    state,
    targetEligibility: new Map([
      ["stopped", { mutationAllowed: false, mutationReason: blockedReason }],
    ]),
  };
};

const renderDialog = (autoUpdate = createController()) =>
  render(
    <DockerAutoUpdateSettingsSection
      autoUpdate={autoUpdate}
      dockerUpdatesEnabled
    />,
  );

describe("DockerAutoUpdateSettingsSection eligibility", () => {
  it("labels the scheduler as update checks and explains its scope", () => {
    renderDialog();

    expect(screen.getByText("Scheduled update checks")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Enabled — all running containers are checked on schedule",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("checkbox", { name: "Enable scheduled update checks" }),
    ).toBeChecked();
    expect(
      screen.getByText(
        "Choose whether selected containers are updated after each check",
      ),
    ).toBeInTheDocument();
  });

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

  it("selects automatic-update targets centrally from Settings", async () => {
    const autoUpdate = createController();
    autoUpdate.state = {
      ...autoUpdate.state!,
      containers: [
        {
          id: "web-id",
          image: "example/web:latest",
          mutationAllowed: true,
          name: "web",
          selected: false,
          state: "running",
        },
      ],
      options: {
        ...autoUpdate.state!.options,
        container_names: [],
      },
    };
    autoUpdate.targetEligibility = new Map([
      ["web", { mutationAllowed: true }],
    ]);
    const { user } = renderDialog(autoUpdate);

    await user.click(screen.getAllByRole("combobox")[0]);
    await user.click(screen.getByRole("option", { name: "web" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(autoUpdate.saveOptions).toHaveBeenCalledWith(
      expect.objectContaining({ container_names: ["web"] }),
    );
  });

  it("allows saving the same selection in check-only mode", async () => {
    const autoUpdate = createController();
    const { user } = renderDialog(autoUpdate);

    const comboboxes = screen.getAllByRole("combobox");
    await user.click(comboboxes[1]);
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
