import { describe, expect, it, vi } from "vitest";

import type { AutoUpdateState, Timer } from "@/api";

import UpdateSettings, { useUpdateSettingsState } from "./UpdateSettings";

const { render, screen, within } = await import("@/test/render");

const timer = (name: string, active: boolean, next_elapse_usec = 0): Timer => ({
  active_enter_timestamp: 0,
  active_state: active ? "active" : "inactive",
  inactive_enter_timestamp: 0,
  last_trigger_usec: 0,
  load_state: "loaded",
  name,
  next_elapse_usec,
  sub_state: active ? "waiting" : "dead",
  unit: name.replace(".timer", ".service"),
  unit_file_state: active ? "enabled" : "disabled",
});

const aptState = (overrides: Partial<AutoUpdateState["options"]> = {}): AutoUpdateState => ({
  backend: "apt-unattended",
  options: {
    download_only: false,
    enabled: true,
    exclude_packages: [],
    frequency: "daily",
    reboot_policy: "if_needed",
    scope: "all",
    ...overrides,
  },
});

const settingsState = (
  serverState: AutoUpdateState,
  timers: Timer[],
  dirty = false,
): ReturnType<typeof useUpdateSettingsState> => ({
  currentExcludeInput: serverState.options.exclude_packages.join(", "),
  currentOptions: serverState.options,
  dirty,
  loading: false,
  reset: vi.fn(),
  runtimeError: false,
  runtimeLoading: false,
  save: vi.fn(),
  saving: false,
  serverState,
  setDraftOverrides: vi.fn(),
  setExcludeInputOverride: vi.fn(),
  timers,
});

describe("UpdateSettings", () => {
  it("shows the live apt timers separately from the saved configuration", () => {
    const nextRun = Date.now() * 1000 + 60_000_000;
    render(
      <UpdateSettings
        state={settingsState(aptState(), [
          timer("apt-daily.timer", true, nextRun),
          timer("apt-daily-upgrade.timer", true, nextRun),
        ])}
      />,
    );

    const runtime = screen.getByLabelText("Automatic update runtime status");
    expect(within(runtime).getByText("Operational")).toBeInTheDocument();
    expect(within(runtime).getByText("apt-daily.timer")).toBeInTheDocument();
    expect(within(runtime).getByText("apt-daily-upgrade.timer")).toBeInTheDocument();
    expect(screen.getByText("Applied configuration")).toBeInTheDocument();
    expect(screen.getByText("All updates (including extras)")).toBeInTheDocument();
    expect(screen.getByText("Download and install")).toBeInTheDocument();
    expect(within(runtime).getByText("Next scheduled run")).toBeInTheDocument();
    expect(screen.getByLabelText("About unattended-upgrades.service")).toHaveTextContent(
      "PyGIDeprecationWarning",
    );
    expect(screen.getByLabelText("About unattended-upgrades.service")).toHaveTextContent(
      "not an update failure",
    );
    expect(
      within(screen.getByLabelText("Automatic updates master control")).getByText(
        "Enabled — updates follow the schedule and policy below",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("Apply offline at next reboot")).not.toBeInTheDocument();
  });

  it("shows when the apt scheduler is disabled", () => {
    render(
      <UpdateSettings
        state={settingsState(aptState({ enabled: false }), [
          timer("apt-daily.timer", false),
          timer("apt-daily-upgrade.timer", false),
        ])}
      />,
    );

    expect(
      within(screen.getByLabelText("Automatic update runtime status")).getByText("Disabled"),
    ).toBeInTheDocument();
  });

  it("flags mismatched runtime state and unsaved edits", () => {
    render(
      <UpdateSettings
        state={settingsState(
          aptState(),
          [timer("apt-daily.timer", true), timer("apt-daily-upgrade.timer", false)],
          true,
        )}
      />,
    );

    expect(
      within(screen.getByLabelText("Automatic update runtime status")).getByText("Needs attention"),
    ).toBeInTheDocument();
    expect(screen.getByText("Unsaved edits")).toBeInTheDocument();
  });
});
