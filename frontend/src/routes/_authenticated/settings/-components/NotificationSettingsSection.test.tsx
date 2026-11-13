import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AutoUpdateState,
  NotificationSettings,
  NotificationSettingsView,
} from "@/api";
import { render } from "@/test/render";

import NotificationSettingsSection from "./NotificationSettingsSection";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  getAutoUpdates: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
  });
  return {
    ...actual,
    useCallMutation,
    linuxio: mockMutationHooks(
      {
        ...actual.linuxio,
        alerts: {
          ...actual.linuxio.alerts,
          get_notifications: {
            queryKey: ["test-get-notifications"],
            queryFn: () => mocks.get(),
          },
        },
        updates: {
          ...actual.linuxio.updates,
          get_auto_updates: {
            queryKey: ["test-get-auto-updates"],
            queryFn: () => mocks.getAutoUpdates(),
          },
        },
      },
      useCallMutation,
    ),
  };
});

const saved: NotificationSettingsView = {
  dockerUpdates: "on_failure",
  dockerUpdatesAvailable: false,
  minSeverity: "error",
  webhookUrl: "",
  smtpHost: "smtp.example.com",
  smtpPort: 587,
  smtpUsername: "nas",
  emailFrom: "nas@example.com",
  emailTo: ["me@example.com"],
  smtpPasswordSet: true,
  levels: {
    info: { browser: true, email: false, webhook: false },
    warning: { browser: true, email: false, webhook: false },
    error: { browser: true, email: true, webhook: true },
  },
  report: {
    docker_updates: "failures",
    scheduled_scripts: "failures",
    snapshots: "failures",
    resync: "changes",
    browser_jobs: "failures",
  },
};

const autoUpdates: AutoUpdateState = {
  backend: "apt-unattended",
  can_configure: true,
  options: {
    download_only: false,
    enabled: false,
    exclude_packages: [],
    extra_origins: [],
    frequency: "daily",
    notify: "on_failure",
    notify_available: false,
    reboot_policy: "never",
    scope: "security",
  },
  support: {
    download_only: true,
    exclude_packages: true,
    extra_origins: true,
    frequencies: ["hourly", "daily", "weekly"],
    notify_available: true,
    notify_policies: ["on_failure", "on_change", "always"],
    reboot_policies: ["never", "if_needed"],
    scopes: ["security", "updates", "all"],
  },
};

/** What the backend answers when notifications.json cannot be read. */
const unreadable: NotificationSettingsView = {
  ...saved,
  settingsUnreadable: true,
  smtpPasswordSet: false,
};

const { smtpPasswordSet: _set, ...stored } = saved;

/** What Save sends: the whole settings, with the loaded values unless changed. */
const request = (change: Partial<NotificationSettings> = {}) => ({
  ...stored,
  smtpPassword: "",
  ...change,
});

type User = ReturnType<typeof userEvent.setup>;

/** The option names a select offers, read by opening and closing it. */
const optionsOf = async (user: User, select: HTMLElement) => {
  await user.click(select);
  const names = screen
    .getAllByRole("option")
    .map((option) => option.textContent);
  await user.keyboard("{Escape}");
  return names;
};

const choose = async (user: User, label: string, option: string) => {
  await user.click(screen.getByRole("combobox", { name: label }));
  await user.click(screen.getByRole("option", { name: option }));
};

/** The System updates select once the auto update state has loaded. */
const loadedSystemSelect = async () => {
  const select = await screen.findByRole("combobox", {
    name: "System updates",
  });
  await waitFor(() => expect(select).not.toHaveAttribute("aria-disabled"));
  return select;
};

beforeEach(() => {
  mocks.mutations = {};
  mocks.get.mockResolvedValue(saved);
  mocks.getAutoUpdates.mockResolvedValue(autoUpdates);
});

describe("NotificationSettingsSection", () => {
  it("shows the saved settings without the password and sends a test", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    expect(await screen.findByLabelText("SMTP server")).toHaveValue(
      "smtp.example.com",
    );
    expect(screen.getByLabelText("Password")).toHaveValue("");
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "placeholder",
      "Saved; type to replace",
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(
      screen.getByText(
        "Choose what LinuxIO tells you about and where each level is sent.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Each notification is sent once, when it first appears or changes.",
      ),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Send test" }));
    expect(mocks.mutations["alerts.test_notifications"]).toHaveBeenCalled();
  });

  it("saves edits with recipients split into a list and the whole levels and report", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    const to = await screen.findByLabelText("To");
    await user.clear(to);
    await user.type(to, "a@example.com, b@example.com,");
    await user.type(
      screen.getByLabelText("Webhook URL"),
      "https://hooks.example.com/x",
    );
    // A pending edit is saved before it can be tested.
    expect(screen.getByRole("button", { name: "Send test" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations["alerts.set_notifications"]).toHaveBeenCalledWith(
      request({
        webhookUrl: "https://hooks.example.com/x",
        emailTo: ["a@example.com", "b@example.com"],
      }),
      expect.anything(),
    );
  });

  it("has no Send alerts select any more", async () => {
    render(<NotificationSettingsSection />);

    await screen.findByLabelText("SMTP server");
    expect(screen.queryByLabelText("Send alerts")).toBeNull();
  });

  it("ties the Scheduled scripts help line to its select", async () => {
    render(<NotificationSettingsSection />);

    const select = await screen.findByRole("combobox", {
      name: "Scheduled scripts",
    });
    expect(select).toHaveAccessibleDescription(
      "A task can choose differently in its own dialog.",
    );
    expect(
      screen.getByRole("combobox", { name: "Docker updates" }),
    ).not.toHaveAttribute("aria-describedby");
  });

  it("offers each kind of work exactly its own reports", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    const card = await screen.findByRole("region", {
      name: "What to hear about",
    });
    await loadedSystemSelect();
    const kinds = [
      "System updates",
      "Docker updates",
      "Scheduled scripts",
      "Snapshots and replication",
      "Pool resync",
      "Jobs started in the browser",
    ];
    const selects = within(card).getAllByRole("combobox");
    expect(selects).toHaveLength(kinds.length);
    kinds.forEach((kind, index) =>
      expect(selects[index]).toHaveAccessibleName(kind),
    );

    const offered = [];
    for (const select of selects) {
      offered.push(await optionsOf(user, select));
    }
    expect(offered).toEqual([
      ["Failures only", "Also when something changed", "Every run"],
      ["Failures only", "Also when something changed", "Every run"],
      ["Failures only", "Every run"],
      ["Failures only", "Also when something changed", "Every run"],
      ["Failures only", "Also when a pool is whole again"],
      ["Failures only", "Also long jobs that finish"],
    ]);
    expect(
      within(card).getByRole("combobox", { name: "Pool resync" }),
    ).toHaveTextContent("Also when a pool is whole again");
    expect(
      within(card).getByText(
        "A task can choose differently in its own dialog.",
      ),
    ).toBeInTheDocument();
    expect(
      within(card).getByText(
        "Failures are always reported. A run cut short by a shutdown or a reboot is a warning. Choose what else to hear about; system choices save as you change them.",
      ),
    ).toBeInTheDocument();
    expect(
      within(card).getByText(
        "Storage health is always raised: missing disks and errors as alerts, low space and an unmounted pool as warnings.",
      ),
    ).toBeInTheDocument();
    expect(
      within(card).getByText(
        "Waiting updates are notices: tick Notices for Email or Webhook to have them sent.",
      ),
    ).toBeInTheDocument();
  });

  it("holds the System updates row in place, disabled, while its state loads", async () => {
    let answer: (state: AutoUpdateState) => void = () => {};
    mocks.getAutoUpdates.mockReturnValue(
      new Promise<AutoUpdateState>((resolve) => {
        answer = resolve;
      }),
    );
    render(<NotificationSettingsSection />);

    const card = await screen.findByRole("region", {
      name: "What to hear about",
    });
    const [system, ...others] = within(card).getAllByRole("combobox");
    expect(system).toHaveAccessibleName("System updates");
    expect(system).toHaveAttribute("aria-disabled", "true");
    expect(others).toHaveLength(5);

    answer(autoUpdates);
    await waitFor(() => expect(system).not.toHaveAttribute("aria-disabled"));
    expect(system).toHaveTextContent("Failures only");
    expect(within(card).getAllByRole("combobox")[0]).toBe(system);
  });

  it("shows no System updates row when its state cannot be read", async () => {
    mocks.getAutoUpdates.mockRejectedValue(new Error("not available"));
    render(<NotificationSettingsSection />);

    await screen.findByRole("combobox", { name: "Docker updates" });
    await waitFor(() =>
      expect(screen.queryByLabelText("System updates")).toBeNull(),
    );
    expect(screen.getAllByRole("combobox")).toHaveLength(5);
  });

  it("offers only the system update policies this machine's provider has", async () => {
    const user = userEvent.setup();
    mocks.getAutoUpdates.mockResolvedValue({
      ...autoUpdates,
      backend: "dnf-automatic",
      support: {
        ...autoUpdates.support,
        notify_policies: ["on_failure", "always"],
      },
    });
    render(<NotificationSettingsSection />);

    const system = await loadedSystemSelect();
    expect(await optionsOf(user, system)).toEqual([
      "Failures only",
      "Every run",
    ]);
  });

  it("saves the system update policies as they change", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    await loadedSystemSelect();
    await choose(user, "System updates", "Also when something changed");
    expect(mocks.mutations["updates.set_auto_updates"]).toHaveBeenCalledWith({
      ...autoUpdates.options,
      notify: "on_change",
    });
    await user.click(
      screen.getByLabelText("Tell me when system updates are waiting"),
    );
    expect(
      mocks.mutations["updates.set_auto_updates"],
    ).toHaveBeenLastCalledWith({
      ...autoUpdates.options,
      notify_available: true,
    });
    // Those save on their own: the delivery form is still clean.
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(mocks.mutations["alerts.set_notifications"]).not.toHaveBeenCalled();
  });

  it("hides the system controls when the backend offers none", async () => {
    mocks.getAutoUpdates.mockResolvedValue({
      ...autoUpdates,
      support: {
        ...autoUpdates.support,
        notify_available: false,
        notify_policies: [],
      },
    });
    render(<NotificationSettingsSection />);

    expect(
      await screen.findByRole("combobox", { name: "Docker updates" }),
    ).toHaveTextContent("Failures only");
    // Held in place while the state loads, then gone.
    await waitFor(() =>
      expect(screen.queryByLabelText("System updates")).toBeNull(),
    );
    expect(
      screen.queryByLabelText("Tell me when system updates are waiting"),
    ).toBeNull();
  });

  it("saves the other kinds and the Docker waiting box only with Save", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    await screen.findByRole("combobox", { name: "Docker updates" });
    await choose(user, "Docker updates", "Every run");
    await choose(
      user,
      "Snapshots and replication",
      "Also when something changed",
    );
    await choose(
      user,
      "Jobs started in the browser",
      "Also long jobs that finish",
    );
    await user.click(
      screen.getByLabelText("Tell me when Docker updates are waiting"),
    );
    expect(mocks.mutations["alerts.set_notifications"]).not.toHaveBeenCalled();
    expect(mocks.mutations["updates.set_auto_updates"]).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations["alerts.set_notifications"]).toHaveBeenCalledWith(
      request({
        dockerUpdatesAvailable: true,
        report: {
          ...saved.report,
          docker_updates: "every_run",
          snapshots: "changes",
          browser_jobs: "long_jobs",
        },
      }),
      expect.anything(),
    );
  });

  it("saves the scheduled scripts and pool resync choices", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    await screen.findByRole("combobox", { name: "Scheduled scripts" });
    await choose(user, "Scheduled scripts", "Every run");
    await choose(user, "Pool resync", "Failures only");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations["alerts.set_notifications"]).toHaveBeenCalledWith(
      request({
        report: {
          ...saved.report,
          scheduled_scripts: "every_run",
          resync: "failures",
        },
      }),
      expect.anything(),
    );
  });

  it("keeps the draft on screen when a save is refused", async () => {
    const user = userEvent.setup();
    // Answer the save the way a 400 would: error, then settled, never success.
    mocks.mutations["alerts.set_notifications"] = vi.fn(
      (
        _settings: unknown,
        options: { onError?: (error: Error) => void; onSettled?: () => void },
      ) => {
        options.onError?.(new Error("invalid notification settings"));
        options.onSettled?.();
      },
    );
    render(<NotificationSettingsSection />);

    await user.type(
      await screen.findByLabelText("Webhook URL"),
      "https://hooks.example.com/x",
    );
    await choose(user, "Pool resync", "Failures only");
    await user.click(
      screen.getByRole("checkbox", { name: "Warnings by webhook" }),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations["alerts.set_notifications"]).toHaveBeenCalledTimes(
      1,
    );
    expect(screen.getByLabelText("Webhook URL")).toHaveValue(
      "https://hooks.example.com/x",
    );
    expect(
      screen.getByRole("combobox", { name: "Pool resync" }),
    ).toHaveTextContent("Failures only");
    expect(
      screen.getByRole("checkbox", { name: "Warnings by webhook" }),
    ).toBeChecked();
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Send test" })).toBeDisabled();
  });

  it("routes each level to its channels and saves the grid with Save", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    const card = await screen.findByRole("region", {
      name: "Where each level goes",
    });
    const grid = within(card).getByRole("table");
    expect(
      within(grid)
        .getAllByRole("columnheader")
        .map((header) => header.textContent),
    ).toEqual(["Browser", "Email", "Webhook"]);
    expect(
      within(grid)
        .getAllByRole("rowheader")
        .map((header) => header.textContent),
    ).toEqual(["Notices", "Warnings", "Alerts"]);
    expect(
      within(grid)
        .getAllByRole("checkbox")
        .map((box) => box.getAttribute("aria-label")),
    ).toEqual([
      "Notices in the browser",
      "Notices by email",
      "Notices by webhook",
      "Warnings in the browser",
      "Warnings by email",
      "Warnings by webhook",
      "Alerts in the browser",
      "Alerts by email",
      "Alerts by webhook",
    ]);

    const alertsInBrowser = within(grid).getByRole("checkbox", {
      name: "Alerts in the browser",
    });
    expect(alertsInBrowser).toBeChecked();
    expect(alertsInBrowser).toBeDisabled();
    // The locked box says why it is locked.
    expect(alertsInBrowser).toHaveAccessibleDescription(
      "Alerts always show in the bell.",
    );
    expect(
      within(grid).getByRole("checkbox", { name: "Alerts by email" }),
    ).not.toHaveAccessibleDescription();
    await user.click(alertsInBrowser);
    expect(alertsInBrowser).toBeChecked();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await user.click(
      within(grid).getByRole("checkbox", { name: "Warnings by email" }),
    );
    // Ticking a box saves nothing on its own.
    expect(mocks.mutations["alerts.set_notifications"]).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mocks.mutations["alerts.set_notifications"]).toHaveBeenCalledWith(
      request({
        levels: {
          ...saved.levels,
          warning: { browser: true, email: true, webhook: false },
        },
      }),
      expect.anything(),
    );
  });

  it("explains the bell and a channel that is ticked but not set up", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    const card = await screen.findByRole("region", {
      name: "Where each level goes",
    });
    expect(
      within(card).getByText("Alerts always show in the bell."),
    ).toBeInTheDocument();
    expect(
      within(card).getByText(
        "A level that is not ticked for Browser does not show in the bell.",
      ),
    ).toBeInTheDocument();
    // Alerts go by webhook, which has no URL; email has its server.
    expect(
      within(card).getByText("The webhook is not set up yet."),
    ).toBeInTheDocument();
    expect(within(card).queryByText("Email is not set up yet.")).toBeNull();

    await user.clear(screen.getByLabelText("SMTP server"));
    expect(
      within(card).getByText("Email is not set up yet."),
    ).toBeInTheDocument();
    await user.click(
      within(card).getByRole("checkbox", { name: "Alerts by email" }),
    );
    expect(within(card).queryByText("Email is not set up yet.")).toBeNull();

    await user.type(
      screen.getByLabelText("Webhook URL"),
      "https://hooks.example.com/x",
    );
    expect(
      within(card).queryByText("The webhook is not set up yet."),
    ).toBeNull();
  });

  it("says nothing about unreported system update failures: a failure is always reported", async () => {
    const user = userEvent.setup();
    // A machine with no policy stored reads as failures only.
    mocks.getAutoUpdates.mockResolvedValue({
      ...autoUpdates,
      options: { ...autoUpdates.options, enabled: true, notify: "on_failure" },
    });
    render(<NotificationSettingsSection />);

    const system = await loadedSystemSelect();
    expect(system).toHaveTextContent("Failures only");
    expect(
      screen.queryByText(
        "System update failures are not reported on this machine.",
      ),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Report failures" }),
    ).toBeNull();
    await user.click(system);
    expect(
      screen.getAllByRole("option").map((option) => option.textContent),
    ).toEqual(["Failures only", "Also when something changed", "Every run"]);
  });

  it("warns of an unreadable settings file and saves the defaults over it", async () => {
    const user = userEvent.setup();
    mocks.get.mockResolvedValue(unreadable);
    render(<NotificationSettingsSection />);

    expect(
      await screen.findByText(
        "The stored notification settings could not be read. Saving replaces them.",
      ),
    ).toBeInTheDocument();
    // On the page from the start, so a note, not an announced alert.
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "placeholder",
      "",
    );
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeEnabled();
    await user.click(save);

    expect(mocks.mutations["alerts.set_notifications"]).toHaveBeenCalledWith(
      request(),
      expect.anything(),
    );
  });

  it("asks for a save before a test while the stored file is unreadable", async () => {
    const user = userEvent.setup();
    mocks.get.mockResolvedValue(unreadable);
    render(<NotificationSettingsSection />);

    const test = await screen.findByRole("button", { name: "Send test" });
    expect(test).toBeDisabled();
    expect(test).toHaveAttribute("title", "Save first");
    await user.click(test);
    expect(mocks.mutations["alerts.test_notifications"]).not.toHaveBeenCalled();
  });
});
