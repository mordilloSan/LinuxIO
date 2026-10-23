import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  useCallMutation,
  type NotificationSettings,
  type NotificationSettingsView,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";

/** The form's view: recipients edited as one comma-separated line. */
type Draft = Omit<NotificationSettings, "emailTo"> & { emailTo: string };

// Built field by field: the view carries smtpPasswordSet, which the strict
// set_notifications decoder would reject if it were spread into the request.
const toDraft = (settings: NotificationSettingsView): Draft => ({
  emailFrom: settings.emailFrom,
  emailTo: settings.emailTo.join(", "),
  minSeverity: settings.minSeverity,
  smtpHost: settings.smtpHost,
  smtpPassword: "",
  smtpPort: settings.smtpPort,
  smtpUsername: settings.smtpUsername,
  webhookUrl: settings.webhookUrl,
});

const fromDraft = (draft: Draft): NotificationSettings => ({
  ...draft,
  emailTo: draft.emailTo
    .split(",")
    .map((address) => address.trim())
    .filter(Boolean),
});

const fieldsStyle = {
  display: "grid",
  gap: "var(--app-space-4)",
  gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
} as const;

/** Where new alerts are sent besides the alerts menu: a webhook and email. */
const NotificationSettingsSection = () => {
  const { data, isPending, error } = useQuery(linuxio.alerts.get_notifications);
  // null until edited: the saved settings are shown as they are.
  const [draft, setDraft] = useState<Draft | null>(null);
  const save = useCallMutation(linuxio.alerts.set_notifications, {
    success: "Notification settings saved",
    error: "Failed to save notification settings",
  });
  const test = useCallMutation(linuxio.alerts.test_notifications, {
    success: "Test notification sent",
    error: "Test notification failed",
  });

  const header = (
    <div>
      <AppTypography fontWeight={600} variant="body1">
        Notifications
      </AppTypography>
      <AppTypography color="text.secondary" variant="caption">
        Send new alerts, such as a failed disk or a degraded pool, by webhook or
        email.
      </AppTypography>
    </div>
  );

  if (isPending) {
    return (
      <div aria-busy>
        {header}
        <ComponentLoader />
      </div>
    );
  }
  if (error || !data) {
    return (
      <div>
        {header}
        <AppAlert severity="error">
          {error?.message ?? "Notification settings could not be read."}
        </AppAlert>
      </div>
    );
  }

  const value = draft ?? toDraft(data);
  const edit = (change: Partial<Draft>) => setDraft({ ...value, ...change });
  const text = (key: keyof Draft, label: string, extra = {}) => (
    <AppTextField
      fullWidth
      label={label}
      onChange={(event) => edit({ [key]: event.target.value })}
      size="small"
      value={String(value[key] ?? "")}
      {...extra}
    />
  );

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-6)",
      }}
    >
      {header}
      <FrostedCard style={{ padding: "var(--app-space-6)" }}>
        <CardIconHeader
          align="flex-start"
          headingVariant="section"
          icon={<Icon height={22} icon="mdi:bell-ring-outline" width={22} />}
          iconTint
          subtitle="Each alert is sent once, when it first appears or changes."
          title="Delivery"
        />
        <div style={fieldsStyle}>
          <AppSelect
            fullWidth
            label="Send alerts"
            onChange={(event) => edit({ minSeverity: event.target.value })}
            value={value.minSeverity}
          >
            <option value="">Off</option>
            <option value="error">Errors only</option>
            <option value="warning">Warnings and errors</option>
            <option value="info">Everything</option>
          </AppSelect>
          {text("webhookUrl", "Webhook URL", {
            helperText:
              "Receives a JSON POST per alert; Slack and Discord webhooks work as is.",
            placeholder: "https://",
          })}
        </div>
        <AppTypography
          style={{ marginTop: "var(--app-space-6)" }}
          variant="subtitle2"
        >
          Email
        </AppTypography>
        <div style={fieldsStyle}>
          {text("smtpHost", "SMTP server", { placeholder: "smtp.example.com" })}
          <AppTextField
            fullWidth
            helperText="465 uses TLS; other ports use STARTTLS when offered."
            label="Port"
            onChange={(event) =>
              edit({ smtpPort: Number(event.target.value) || 0 })
            }
            size="small"
            type="number"
            value={value.smtpPort || ""}
          />
          {text("smtpUsername", "Username")}
          {text("smtpPassword", "Password", {
            autoComplete: "new-password",
            placeholder: data.smtpPasswordSet ? "Saved; type to replace" : "",
            type: "password",
          })}
          {text("emailFrom", "From", { placeholder: "nas@example.com" })}
          {text("emailTo", "To", {
            helperText: "Separate several addresses with commas.",
          })}
        </div>
        <div
          style={{
            display: "flex",
            gap: "var(--app-space-4)",
            justifyContent: "flex-end",
            marginTop: "var(--app-space-6)",
          }}
        >
          <AppButton
            disabled={draft !== null || test.isPending}
            onClick={() => test.mutate(undefined)}
            title={draft ? "Save first" : undefined}
            variant="outlined"
          >
            Send test
          </AppButton>
          <AppButton
            disabled={draft === null || save.isPending}
            onClick={() =>
              save.mutate(fromDraft(value), { onSuccess: () => setDraft(null) })
            }
            variant="contained"
          >
            Save
          </AppButton>
        </div>
      </FrostedCard>
    </div>
  );
};

export default NotificationSettingsSection;
