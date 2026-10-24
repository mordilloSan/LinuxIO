import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NotificationSettingsView } from "@/api";
import { render } from "@/test/render";

import NotificationSettingsSection from "./NotificationSettingsSection";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: (endpoint: { route: string }) => ({
      isPending: false,
      mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
    }),
    linuxio: {
      ...actual.linuxio,
      alerts: {
        ...actual.linuxio.alerts,
        get_notifications: {
          queryKey: ["test-get-notifications"],
          queryFn: () => mocks.get(),
        },
      },
    },
  };
});

const saved: NotificationSettingsView = {
  minSeverity: "error",
  webhookUrl: "",
  smtpHost: "smtp.example.com",
  smtpPort: 587,
  smtpUsername: "nas",
  emailFrom: "nas@example.com",
  emailTo: ["me@example.com"],
  smtpPasswordSet: true,
};

beforeEach(() => {
  mocks.mutations = {};
  mocks.get.mockResolvedValue(saved);
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
});
