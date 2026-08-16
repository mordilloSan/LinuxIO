import { describe, expect, it, vi } from "vitest";

import type { AutoUpdateBackend, AutoUpdateState, Timer } from "@/api";

import type { useUpdateSettingsState } from "./UpdateSettings";
import UpdateSettings from "./UpdateSettings";

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

const fullSupport: AutoUpdateState["support"] = {
  download_only: true,
  exclude_packages: true,
  frequencies: ["hourly", "daily", "weekly"],
  reboot_policies: ["never", "if_needed"],
  scopes: ["security", "updates", "all"],
};

const autoUpdateState = (
  backend: AutoUpdateBackend = "apt-unattended",
  overrides: Partial<AutoUpdateState["options"]> = {},
  stateOverrides: Partial<Omit<AutoUpdateState, "backend" | "options">> = {},
): AutoUpdateState => ({
  backend,
  can_configure: true,
  options: {
    download_only: false,
    enabled: true,
    exclude_packages: [],
    frequency: "daily",
    reboot_policy: "if_needed",
    scope: "all",
    ...overrides,
  },
  support: fullSupport,
  ...stateOverrides,
});

const aptState = (
  overrides: Partial<AutoUpdateState["options"]> = {},
): AutoUpdateState => autoUpdateState("apt-unattended", overrides);

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
    expect(
      within(runtime).getByText("apt-daily-upgrade.timer"),
    ).toBeInTheDocument();
    expect(screen.getByText("Applied configuration")).toBeInTheDocument();
    expect(
      within(
        screen.getByLabelText("Saved automatic update configuration"),
      ).getByText("All enabled repositories"),
    ).toBeInTheDocument();
    expect(screen.getByText("Download and install")).toBeInTheDocument();
    expect(within(runtime).getByText("Next scheduled run")).toBeInTheDocument();
    expect(
      screen.getByLabelText("About unattended-upgrades.service"),
    ).toHaveTextContent("PyGIDeprecationWarning");
    expect(
      screen.getByLabelText("About unattended-upgrades.service"),
    ).toHaveTextContent("not an update failure");
    expect(
      within(
        screen.getByLabelText("Automatic updates master control"),
      ).getByText("Enabled — updates follow the schedule and policy below"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Apply offline at next reboot"),
    ).not.toBeInTheDocument();
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
      within(
        screen.getByLabelText("Automatic update runtime status"),
      ).getByText("Disabled"),
    ).toBeInTheDocument();
  });

  it("allows apt-daily.timer to remain active when automatic installs are disabled", () => {
    render(
      <UpdateSettings
        state={settingsState(aptState({ enabled: false }), [
          timer("apt-daily.timer", true),
          timer("apt-daily-upgrade.timer", false),
        ])}
      />,
    );

    expect(
      within(
        screen.getByLabelText("Automatic update runtime status"),
      ).getByText("Disabled"),
    ).toBeInTheDocument();
  });

  it.each([
    {
      backend: "apt-unattended" as const,
      timerNames: ["apt-daily.timer", "apt-daily-upgrade.timer"],
    },
    {
      backend: "mintupdate-automation" as const,
      timerNames: ["mintupdate-automation-upgrade.timer"],
    },
    {
      backend: "dnf-automatic" as const,
      timerNames: ["dnf-automatic.timer"],
    },
    {
      backend: "dnf5-automatic" as const,
      timerNames: ["dnf5-automatic.timer"],
    },
  ])("shows the managed timers for $backend", ({ backend, timerNames }) => {
    render(
      <UpdateSettings
        state={settingsState(
          autoUpdateState(backend),
          timerNames.map((name) => timer(name, true)),
        )}
      />,
    );

    const runtime = screen.getByLabelText("Automatic update runtime status");
    expect(within(runtime).getByText("Operational")).toBeInTheDocument();
    for (const name of timerNames) {
      expect(within(runtime).getByText(name)).toBeInTheDocument();
    }
  });

  it("limits Linux Mint controls to its supported options", () => {
    render(
      <UpdateSettings
        state={settingsState(
          autoUpdateState(
            "mintupdate-automation",
            { reboot_policy: "never" },
            {
              support: {
                download_only: false,
                exclude_packages: true,
                frequencies: ["hourly", "daily", "weekly"],
                reboot_policies: ["never"],
                scopes: ["security", "all"],
              },
            },
          ),
          [timer("mintupdate-automation-upgrade.timer", true)],
        )}
      />,
    );

    expect(screen.getByText("Mint Update Manager")).toBeInTheDocument();
    expect(screen.getByLabelText("Reboot policy")).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(screen.getByLabelText("Reboot policy")).toHaveTextContent(
      "Never reboot",
    );
    expect(
      screen.getByLabelText("Download only without automatic installation"),
    ).toBeDisabled();
    expect(screen.getByLabelText("Package exclusions")).toBeEnabled();
  });

  it("exposes DNF5 reboot policies", async () => {
    const { user } = render(
      <UpdateSettings
        state={settingsState(
          autoUpdateState(
            "dnf5-automatic",
            {},
            {
              support: {
                ...fullSupport,
                reboot_policies: ["never", "if_needed", "always"],
                scopes: ["security", "all"],
              },
            },
          ),
          [timer("dnf5-automatic.timer", true)],
        )}
      />,
    );

    const rebootPolicy = screen.getByLabelText("Reboot policy");
    expect(rebootPolicy).not.toHaveAttribute("aria-disabled");
    await user.click(rebootPolicy);
    expect(await screen.findAllByRole("option")).toHaveLength(3);
  });

  it("disables configuration when the native provider is unavailable", () => {
    render(
      <UpdateSettings
        state={settingsState(
          autoUpdateState(
            "dnf-automatic",
            { reboot_policy: "never", scope: "security" },
            {
              can_configure: false,
              notes: ["Install dnf-automatic to configure automatic updates."],
              support: {
                ...fullSupport,
                exclude_packages: false,
                reboot_policies: ["never"],
                scopes: ["security", "updates"],
              },
            },
          ),
          [timer("dnf-automatic.timer", false)],
          true,
        )}
      />,
    );

    expect(screen.getByLabelText("Enable automatic updates")).toBeDisabled();
    for (const label of ["Frequency", "Update scope", "Reboot policy"]) {
      expect(screen.getByLabelText(label)).toHaveAttribute(
        "aria-disabled",
        "true",
      );
    }
    expect(
      screen.getByLabelText("Download only without automatic installation"),
    ).toBeDisabled();
    expect(screen.getByLabelText("Package exclusions")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
    expect(
      screen.getByText("Install dnf-automatic to configure automatic updates."),
    ).toBeInTheDocument();
  });

  it("flags mismatched runtime state and unsaved edits", () => {
    render(
      <UpdateSettings
        state={settingsState(
          aptState(),
          [
            timer("apt-daily.timer", true),
            timer("apt-daily-upgrade.timer", false),
          ],
          true,
        )}
      />,
    );

    expect(
      within(
        screen.getByLabelText("Automatic update runtime status"),
      ).getByText("Needs attention"),
    ).toBeInTheDocument();
    expect(screen.getByText("Unsaved edits")).toBeInTheDocument();
  });
});
