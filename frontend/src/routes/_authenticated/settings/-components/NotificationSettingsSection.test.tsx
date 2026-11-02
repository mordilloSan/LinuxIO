import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AutoUpdateState, NotificationSettingsView } from "@/api";
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
    notify_policies: ["never", "on_failure", "on_change", "always"],
    reboot_policies: ["never", "if_needed"],
    scopes: ["security", "updates", "all"],
  },
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
    await user.click(screen.getByRole("button", { name: "Send test" }));
    expect(mocks.mutations["alerts.test_notifications"]).toHaveBeenCalled();
  });

  it("saves edits with recipients split into a list", async () => {
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

    const { smtpPasswordSet: _set, ...savedSettings } = saved;
    expect(mocks.mutations["alerts.set_notifications"]).toHaveBeenCalledWith(
      {
        ...savedSettings,
        smtpPassword: "",
        webhookUrl: "https://hooks.example.com/x",
        emailTo: ["a@example.com", "b@example.com"],
      },
      expect.anything(),
    );
  });

  it("saves the system update policies as they change", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    await user.click(await screen.findByLabelText("System update runs"));
    await user.click(screen.getByRole("option", { name: "On install" }));
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
      await screen.findByLabelText("Docker update runs"),
    ).toHaveTextContent("On failure");
    expect(screen.queryByLabelText("System update runs")).toBeNull();
  });

  it("saves the Docker update policies with the delivery settings", async () => {
    const user = userEvent.setup();
    render(<NotificationSettingsSection />);

    await user.click(await screen.findByLabelText("Docker update runs"));
    await user.click(screen.getByRole("option", { name: "On install" }));
    await user.click(
      screen.getByLabelText("Tell me when Docker updates are waiting"),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    const { smtpPasswordSet: _set, ...savedSettings } = saved;
    expect(mocks.mutations["alerts.set_notifications"]).toHaveBeenCalledWith(
      {
        ...savedSettings,
        smtpPassword: "",
        dockerUpdates: "on_change",
        dockerUpdatesAvailable: true,
      },
      expect.anything(),
    );
  });
});
