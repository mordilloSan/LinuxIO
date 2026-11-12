import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";

import {
  type AutoUpdateNotify,
  type AutoUpdateOptions,
  linuxio,
  type NotificationChannels,
  type NotificationSettings,
  type NotificationSettingsView,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";

type Level = "info" | "warning" | "error";
type Channel = keyof NotificationChannels;
type Kind =
  | "docker_updates"
  | "scheduled_scripts"
  | "snapshots"
  | "resync"
  | "browser_jobs";

/**
 * The form's view: recipients edited as one comma-separated line, and every
 * level and kind named, so a save always sends the whole levels and report.
 */
type Draft = Omit<NotificationSettings, "emailTo" | "levels" | "report"> & {
  emailTo: string;
  levels: Record<Level, NotificationChannels>;
  report: Record<Kind, string>;
};

const channelsOf = ({
  browser,
  email,
  webhook,
}: NotificationChannels): NotificationChannels => ({ browser, email, webhook });

// Built field by field: the view carries smtpPasswordSet and
// settingsUnreadable, which the strict set_notifications decoder would reject
// if they were spread into the request.
const toDraft = (settings: NotificationSettingsView): Draft => ({
  dockerUpdates: settings.dockerUpdates,
  dockerUpdatesAvailable: settings.dockerUpdatesAvailable,
  emailFrom: settings.emailFrom,
  emailTo: settings.emailTo.join(", "),
  levels: {
    info: channelsOf(settings.levels.info),
    warning: channelsOf(settings.levels.warning),
    // Alerts always reach the browser.
    error: { ...channelsOf(settings.levels.error), browser: true },
  },
  minSeverity: settings.minSeverity,
  report: {
    docker_updates: settings.report.docker_updates,
    scheduled_scripts: settings.report.scheduled_scripts,
    snapshots: settings.report.snapshots,
    resync: settings.report.resync,
    browser_jobs: settings.report.browser_jobs,
  },
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

interface DraftProps {
  edit: (change: Partial<Draft>) => void;
  value: Draft;
}

const fieldsStyle = {
  display: "grid",
  gap: "var(--app-space-4)",
  gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
} as const;

const notesStyle = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-2)",
  marginTop: "var(--app-space-6)",
} as const;

const failuresOnly = { value: "failures", label: "Failures only" };
const whenChanged = { value: "changes", label: "Also when something changed" };
const everyRun = { value: "every_run", label: "Every run" };

/** Each kind offers only the reports that can differ for it. */
const reportKinds: {
  kind: Kind;
  label: string;
  choices: { value: string; label: string }[];
  help?: string;
}[] = [
  {
    kind: "docker_updates",
    label: "Docker updates",
    choices: [failuresOnly, whenChanged, everyRun],
  },
  {
    kind: "scheduled_scripts",
    label: "Scheduled scripts",
    choices: [failuresOnly, everyRun],
    help: "A task can choose differently in its own dialog.",
  },
  {
    kind: "snapshots",
    label: "Snapshots and replication",
    choices: [failuresOnly, whenChanged, everyRun],
  },
  {
    kind: "resync",
    label: "Pool resync",
    choices: [
      failuresOnly,
      { value: "changes", label: "Also when a pool is whole again" },
    ],
  },
  {
    kind: "browser_jobs",
    label: "Jobs started in the browser",
    choices: [
      failuresOnly,
      { value: "long_jobs", label: "Also long jobs that finish" },
    ],
  },
];

type SystemPolicy = Exclude<AutoUpdateNotify, "never">;

/** The provider's drop-in policies, in the words the other kinds use. */
const systemPolicyLabels: Record<SystemPolicy, string> = {
  on_failure: failuresOnly.label,
  on_change: whenChanged.label,
  always: everyRun.label,
};

// No choice silences a failure, so "never" is never offered.
const isSystemPolicy = (policy: AutoUpdateNotify): policy is SystemPolicy =>
  policy !== "never";

const caption = (text: string, id?: string) => (
  <AppTypography color="text.secondary" component="p" id={id} variant="caption">
    {text}
  </AppTypography>
);

/**
 * What each kind of work reports besides its failures. The system update
 * choices live with the automatic update configuration, so they save as they
 * change; the others are part of the draft and save with Save.
 */
const ReportCard = ({ edit, value }: DraftProps) => {
  const { data: auto, isPending: autoPending } = useQuery(
    linuxio.updates.get_auto_updates,
  );
  const saveAuto = linuxio.updates.set_auto_updates.useMutation({
    success: "System update notifications saved",
    error: "Failed to save system update notifications",
  });
  const policies = auto?.support.notify_policies.filter(isSystemPolicy) ?? [];
  // Held in place, disabled, while loading, so the selects after it do not
  // move; gone once the machine turns out to offer no policy.
  const showSystem = autoPending || policies.length > 0;
  const systemDisabled = !auto?.can_configure || saveAuto.isPending;
  const applyAuto = (change: Partial<AutoUpdateOptions>) => {
    if (auto) saveAuto.mutate({ ...auto.options, ...change });
  };

  return (
    <FrostedCard
      aria-label="What to hear about"
      role="region"
      style={{ padding: "var(--app-space-6)" }}
    >
      <CardIconHeader
        align="flex-start"
        headingVariant="section"
        icon={<Icon height={22} icon="mdi:bell-outline" width={22} />}
        iconTint
        subtitle="Failures are always reported. A run cut short by a shutdown or a reboot is a warning. Choose what else to hear about; system choices save as you change them."
        title="What to hear about"
      />
      <div style={fieldsStyle}>
        {showSystem ? (
          <AppSelect
            disabled={systemDisabled}
            fullWidth
            label="System updates"
            onChange={(event) =>
              applyAuto({ notify: event.target.value as AutoUpdateNotify })
            }
            value={auto?.options.notify ?? ""}
          >
            {policies.map((policy) => (
              <option key={policy} value={policy}>
                {systemPolicyLabels[policy]}
              </option>
            ))}
          </AppSelect>
        ) : null}
        {reportKinds.map(({ kind, label, choices, help }) => (
          <div key={kind}>
            <AppSelect
              fullWidth
              label={label}
              onChange={(event) =>
                edit({
                  report: { ...value.report, [kind]: event.target.value },
                })
              }
              value={value.report[kind]}
            >
              {choices.map((choice) => (
                <option key={choice.value} value={choice.value}>
                  {choice.label}
                </option>
              ))}
            </AppSelect>
            {help ? caption(help) : null}
          </div>
        ))}
      </div>
      <div style={{ ...fieldsStyle, marginTop: "var(--app-space-6)" }}>
        {auto?.support.notify_available ? (
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={auto.options.notify_available}
                disabled={systemDisabled}
                onChange={(event) =>
                  applyAuto({ notify_available: event.target.checked })
                }
                size="small"
              />
            }
            label="Tell me when system updates are waiting"
          />
        ) : null}
        <AppFormControlLabel
          control={
            <AppCheckbox
              checked={value.dockerUpdatesAvailable}
              onChange={(event) =>
                edit({ dockerUpdatesAvailable: event.target.checked })
              }
              size="small"
            />
          }
          label="Tell me when Docker updates are waiting"
        />
      </div>
      <div style={notesStyle}>
        {caption(
          "Waiting updates are notices: tick Notices for Email or Webhook to have them sent.",
        )}
        {caption(
          "Storage health is always raised: missing disks and errors as alerts, low space and an unmounted pool as warnings.",
        )}
        {caption(
          "Docker needs its update schedule switched on; Check only mode is enough.",
        )}
      </div>
    </FrostedCard>
  );
};

const levelRows: { level: Level; label: string }[] = [
  { level: "info", label: "Notices" },
  { level: "warning", label: "Warnings" },
  { level: "error", label: "Alerts" },
];

const channelColumns: { channel: Channel; label: string; by: string }[] = [
  { channel: "browser", label: "Browser", by: "in the browser" },
  { channel: "email", label: "Email", by: "by email" },
  { channel: "webhook", label: "Webhook", by: "by webhook" },
];

const cellStyle = {
  padding: "var(--app-space-4) var(--app-space-8)",
  textAlign: "center",
} as const;

/** Which channels carry each level; alerts always reach the browser. */
const LevelsCard = ({ edit, value }: DraftProps) => {
  const lockedReasonId = useId();
  const ticked = (channel: Channel) =>
    levelRows.some(({ level }) => value.levels[level][channel]);
  const setChannel = (level: Level, channel: Channel, on: boolean) =>
    edit({
      levels: {
        ...value.levels,
        [level]: { ...value.levels[level], [channel]: on },
      },
    });

  return (
    <FrostedCard
      aria-label="Where each level goes"
      role="region"
      style={{ padding: "var(--app-space-6)" }}
    >
      <CardIconHeader
        align="flex-start"
        headingVariant="section"
        icon={<Icon height={22} icon="mdi:tune-variant" width={22} />}
        iconTint
        subtitle="Choose which channels carry notices, warnings and alerts."
        title="Where each level goes"
      />
      <table aria-label="Channels for each level">
        <thead>
          <tr>
            <td />
            {channelColumns.map(({ channel, label }) => (
              <th key={channel} scope="col" style={cellStyle}>
                <AppTypography
                  component="span"
                  fontWeight={600}
                  variant="body2"
                >
                  {label}
                </AppTypography>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {levelRows.map(({ level, label }) => (
            <tr key={level}>
              <th scope="row" style={{ ...cellStyle, textAlign: "start" }}>
                <AppTypography
                  component="span"
                  fontWeight={500}
                  variant="body2"
                >
                  {label}
                </AppTypography>
              </th>
              {channelColumns.map(({ channel, by }) => {
                const locked = level === "error" && channel === "browser";
                return (
                  <td key={channel} style={cellStyle}>
                    <AppCheckbox
                      aria-describedby={locked ? lockedReasonId : undefined}
                      aria-label={`${label} ${by}`}
                      checked={value.levels[level][channel]}
                      disabled={locked}
                      onChange={(event) =>
                        setChannel(level, channel, event.target.checked)
                      }
                      size="small"
                    />
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <div style={notesStyle}>
        {caption("Alerts always show in the bell.", lockedReasonId)}
        {caption(
          "A level that is not ticked for Browser does not show in the bell.",
        )}
        {ticked("email") && !value.smtpHost.trim()
          ? caption("Email is not set up yet.")
          : null}
        {ticked("webhook") && !value.webhookUrl.trim()
          ? caption("The webhook is not set up yet.")
          : null}
      </div>
    </FrostedCard>
  );
};

type TextField =
  | "emailFrom"
  | "emailTo"
  | "smtpHost"
  | "smtpPassword"
  | "smtpUsername"
  | "webhookUrl";

interface DeliveryCardProps extends DraftProps {
  passwordSet: boolean;
  testDisabled: boolean;
}

/** The webhook and the email account the levels are sent through. */
const DeliveryCard = ({
  edit,
  passwordSet,
  testDisabled,
  value,
}: DeliveryCardProps) => {
  const test = linuxio.alerts.test_notifications.useMutation({
    success: "Test notification sent",
    error: "Test notification failed",
  });
  const text = (key: TextField, label: string, extra = {}) => (
    <AppTextField
      fullWidth
      label={label}
      onChange={(event) => edit({ [key]: event.target.value })}
      size="small"
      value={value[key] ?? ""}
      {...extra}
    />
  );

  return (
    <FrostedCard
      aria-label="Delivery"
      role="region"
      style={{ padding: "var(--app-space-6)" }}
    >
      <CardIconHeader
        align="flex-start"
        headingVariant="section"
        icon={<Icon height={22} icon="mdi:bell-ring-outline" width={22} />}
        iconTint
        subtitle="Each notification is sent once, when it first appears or changes."
        title="Delivery"
      />
      <div style={fieldsStyle}>
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
          placeholder: passwordSet ? "Saved; type to replace" : "",
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
          justifyContent: "flex-end",
          marginTop: "var(--app-space-6)",
        }}
      >
        <AppButton
          disabled={testDisabled || test.isPending}
          onClick={() => test.mutate(undefined)}
          title={testDisabled ? "Save first" : undefined}
          variant="outlined"
        >
          Send test
        </AppButton>
      </div>
    </FrostedCard>
  );
};

/** What each kind of work reports and where each level is sent. */
const NotificationSettingsSection = () => {
  const { data, isPending, error } = useQuery(linuxio.alerts.get_notifications);
  // null until edited: the saved settings are shown as they are.
  const [draft, setDraft] = useState<Draft | null>(null);
  const save = linuxio.alerts.set_notifications.useMutation({
    success: "Notification settings saved",
    error: "Failed to save notification settings",
  });

  const header = (
    <div>
      <AppTypography fontWeight={600} variant="body1">
        Notifications
      </AppTypography>
      <AppTypography color="text.secondary" variant="caption">
        Choose what LinuxIO tells you about and where each level is sent.
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
  // An unreadable file holds the defaults shown here, so saving them is a
  // change, and a test of the stored settings has to wait for it.
  const unsaved = draft !== null || data.settingsUnreadable === true;

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-6)",
      }}
    >
      {header}
      {data.settingsUnreadable ? (
        <AppAlert severity="warning">
          The stored notification settings could not be read. Saving replaces
          them.
        </AppAlert>
      ) : null}
      <ReportCard edit={edit} value={value} />
      <LevelsCard edit={edit} value={value} />
      <DeliveryCard
        edit={edit}
        passwordSet={data.smtpPasswordSet}
        testDisabled={unsaved}
        value={value}
      />
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <AppButton
          disabled={!unsaved || save.isPending}
          onClick={() =>
            save.mutate(fromDraft(value), { onSuccess: () => setDraft(null) })
          }
          variant="contained"
        >
          Save
        </AppButton>
      </div>
    </div>
  );
};

export default NotificationSettingsSection;
