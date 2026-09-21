import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import {
  linuxio,
  useCallMutation,
  type ScheduleOptions,
  type ScheduleStatus,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import PathPickerField from "@/components/ui/PathPickerField";
import { useScopedToast } from "@/hooks/useScopedToast";

import {
  frequencies,
  parseScheduleTiming,
  scheduleCalendar,
  scheduleSummary,
  weekdays,
  type ScheduleTiming,
} from "./scheduleTiming";

type Draft = ScheduleOptions & { id?: string; enabled: boolean };
const initialDraft: Draft = {
  name: "",
  script: "#!/usr/bin/env bash\nset -euo pipefail\n",
  user: "",
  arguments: [],
  on_calendar: "*-*-* 03:00:00",
  timezone: "",
  persistent: false,
  timeout_seconds: 300,
  working_directory: "",
  enabled: false,
};
const toastMeta = {
  label: "Scheduled tasks",
  to: "/services/schedules",
} as const;

export const createOptions = (draft: Draft): ScheduleOptions => ({
  name: draft.name,
  script: draft.script,
  user: draft.user,
  arguments: draft.arguments,
  on_calendar: draft.on_calendar,
  timezone: draft.timezone,
  persistent: draft.persistent,
  timeout_seconds: draft.timeout_seconds,
  working_directory: draft.working_directory,
});
export const canStop = (status: ScheduleStatus) =>
  status.can_stop && Boolean(status.invocation_id);

interface Props {
  listQueryOptions: typeof linuxio.schedules.list;
  selectedQueryOptions: ReturnType<typeof linuxio.schedules.get> | undefined;
  selected?: string;
  onSelectedChange: (id: string | null) => void;
}

export default function SchedulesTab({
  listQueryOptions,
  selectedQueryOptions,
  selected,
  onSelectedChange,
}: Props) {
  const { data } = useSuspenseQuery({
    ...listQueryOptions,
    refetchInterval: 5000,
  });
  const navigate = useNavigate();
  const selectedQuery = useQuery({
    ...(selectedQueryOptions ?? linuxio.schedules.get({ id: "" })),
    enabled: Boolean(selected),
    refetchInterval: 5000,
  });
  const schedules = data.schedules;
  const unavailable = !data.available;
  const [draft, setDraft] = useState<Draft | null>(null);
  const toast = useScopedToast(toastMeta);
  const { mutate: enable } = useCallMutation(linuxio.schedules.enable, {
    success: () => toast.success("Task enabled"),
    error: "Failed to enable task",
    toast: toastMeta,
  });
  const { mutate: disable } = useCallMutation(linuxio.schedules.disable, {
    success: () => toast.success("Task disabled"),
    error: "Failed to disable task",
    toast: toastMeta,
  });
  const { mutate: runNow } = useCallMutation(linuxio.schedules.run_now, {
    success: () => toast.success("Task queued"),
    error: "Failed to queue task",
    toast: toastMeta,
  });
  const { mutate: stop } = useCallMutation(linuxio.schedules.stop, {
    success: () => toast.success("Stop requested"),
    error: "Failed to stop task",
    toast: toastMeta,
  });
  const { mutate: remove } = useCallMutation(linuxio.schedules.delete, {
    success: () => {
      toast.success("Task deleted");
      onSelectedChange(null);
    },
    error: "Failed to delete task",
    toast: toastMeta,
  });
  const { mutate: create, isPending: creating } = useCallMutation(
    linuxio.schedules.create,
    {
      success: () => {
        toast.success("Task created");
        setDraft(null);
      },
      error: "Failed to create task",
      toast: toastMeta,
    },
  );
  const { mutate: update, isPending: updating } = useCallMutation(
    linuxio.schedules.update,
    {
      success: () => {
        toast.success("Task updated");
        setDraft(null);
      },
      error: "Failed to update task",
      toast: toastMeta,
    },
  );
  const edit = (status: ScheduleStatus) =>
    setDraft({
      ...status.definition.options,
      id: status.definition.id,
      enabled: status.definition.enabled,
    });

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-12)",
      }}
    >
      <div style={{ display: "flex", justifyContent: "space-between" }}>
        <AppTypography variant="h5">Scheduled tasks</AppTypography>
        <AppButton
          disabled={unavailable}
          onClick={() => setDraft({ ...initialDraft })}
          variant="contained"
        >
          Create task
        </AppButton>
      </div>
      <AppTypography color="text.secondary">
        Scripts run as the selected user. New tasks start disabled; overlapping
        activations do not start another run.
      </AppTypography>
      {!data.available && (
        <AppTypography color="error">
          {data.error ?? "Scheduled execution is unavailable on this host."}
        </AppTypography>
      )}
      <div style={{ flex: "1 1 0", minHeight: 0 }}>
        <AppVirtualTable
          ariaLabel="Scheduled tasks"
          columns={columns({
            unavailable,
            onEdit: edit,
            onToggle: (s) =>
              s.definition.enabled
                ? disable({ id: s.definition.id })
                : enable({ id: s.definition.id }),
            onRun: (id) => runNow({ id }),
            onDelete: (s) => {
              if (window.confirm(`Delete ${s.definition.options.name}?`))
                remove({ id: s.definition.id });
            },
          })}
          data={schedules}
          emptyMessage="No scheduled tasks."
          fillAvailable
          getRowId={(row) => row.definition.id}
          onRowClick={({ original }) =>
            onSelectedChange(original.definition.id)
          }
        />
      </div>
      {selected && selectedQuery.error && (
        <AppTypography color="error">
          {selectedQuery.error.message}
        </AppTypography>
      )}
      {selected && selectedQuery.data && (
        <Details
          status={selectedQuery.data}
          onClose={() => onSelectedChange(null)}
          onStop={() =>
            stop({
              id: selected,
              invocation_id: selectedQuery.data.invocation_id,
            })
          }
          onEdit={() => edit(selectedQuery.data)}
          onLogs={() =>
            navigate({
              to: "/logs",
              search: { unit: selectedQuery.data.unit_name },
            })
          }
          onLatestLogs={
            selectedQuery.data.invocation_id
              ? () =>
                  navigate({
                    to: "/logs",
                    search: {
                      unit: selectedQuery.data.unit_name,
                      invocationId: selectedQuery.data.invocation_id,
                    },
                  })
              : undefined
          }
        />
      )}
      {draft && (
        <ScheduleDialog
          draft={draft}
          busy={creating || updating}
          onClose={() => setDraft(null)}
          onSave={(value) =>
            value.id
              ? update({ id: value.id, options: createOptions(value) })
              : create({ options: createOptions(value) })
          }
        />
      )}
    </div>
  );
}

function columns(actions: {
  unavailable: boolean;
  onEdit: (s: ScheduleStatus) => void;
  onToggle: (s: ScheduleStatus) => void;
  onRun: (id: string) => void;
  onDelete: (s: ScheduleStatus) => void;
}): AppVirtualTableColumnDef<ScheduleStatus>[] {
  return [
    {
      id: "name",
      header: "Name",
      cell: ({ row }) => row.original.definition.options.name,
    },
    {
      id: "user",
      header: "User",
      cell: ({ row }) => row.original.definition.options.user,
    },
    {
      id: "calendar",
      header: "Schedule",
      cell: ({ row }) =>
        `${scheduleSummary(row.original.definition.options.on_calendar)} (${row.original.definition.options.timezone || "UTC"})`,
    },
    {
      id: "state",
      header: "State",
      cell: ({ row }) =>
        row.original.definition.enabled
          ? row.original.active_state
          : "disabled",
    },
    {
      id: "next",
      header: "Next run",
      cell: ({ row }) => row.original.next_run_at ?? "—",
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => {
        const s = row.original;
        return (
          <div
            style={{ display: "flex", gap: 4 }}
            onClick={(e) => e.stopPropagation()}
          >
            <AppButton
              disabled={actions.unavailable}
              onClick={() => actions.onToggle(s)}
              size="small"
            >
              {s.definition.enabled ? "Disable" : "Enable"}
            </AppButton>
            <AppButton
              disabled={actions.unavailable}
              onClick={() => actions.onRun(s.definition.id)}
              size="small"
            >
              Run now
            </AppButton>
            <AppButton
              disabled={actions.unavailable}
              onClick={() => actions.onEdit(s)}
              size="small"
            >
              Edit
            </AppButton>
            <AppButton
              disabled={actions.unavailable}
              onClick={() => actions.onDelete(s)}
              size="small"
            >
              Delete
            </AppButton>
          </div>
        );
      },
    },
  ];
}

function Details({
  status,
  onClose,
  onStop,
  onEdit,
  onLogs,
  onLatestLogs,
}: {
  status: ScheduleStatus;
  onClose: () => void;
  onStop: () => void;
  onEdit: () => void;
  onLogs: () => void;
  onLatestLogs?: () => void;
}) {
  return (
    <div
      style={{
        display: "flex",
        gap: 8,
        alignItems: "center",
        flexWrap: "wrap",
      }}
    >
      <AppTypography>
        {status.result || status.active_state} · next{" "}
        {status.next_run_at ?? "—"} · last {status.last_run_at ?? "—"}
        {status.error ? ` · ${status.error}` : ""}
      </AppTypography>
      {canStop(status) && (
        <AppButton onClick={onStop} size="small">
          Stop
        </AppButton>
      )}
      <AppButton onClick={onLogs} size="small">
        Logs
      </AppButton>
      {onLatestLogs && (
        <AppButton onClick={onLatestLogs} size="small">
          Latest run logs
        </AppButton>
      )}
      <AppButton onClick={onEdit} size="small">
        Edit
      </AppButton>
      <AppButton onClick={onClose} size="small">
        Close
      </AppButton>
    </div>
  );
}

function ScheduleDialog({
  draft,
  busy,
  onClose,
  onSave,
}: {
  draft: Draft;
  busy: boolean;
  onClose: () => void;
  onSave: (draft: Draft) => void;
}) {
  const [value, setValue] = useState(draft);
  const [timing, setTiming] = useState(() =>
    parseScheduleTiming(draft.on_calendar),
  );
  const calendar = scheduleCalendar(timing);
  const [argumentText, setArgumentText] = useState(draft.arguments.join("\n"));
  const users = useQuery({ ...linuxio.accounts.list_users });
  const set = <K extends keyof Draft>(key: K, next: Draft[K]) =>
    setValue((current) => ({ ...current, [key]: next }));
  return (
    <GeneralDialog
      aria-label={value.id ? "Edit scheduled task" : "Create scheduled task"}
      fullWidth
      maxWidth="md"
      onClose={busy ? undefined : onClose}
      open
    >
      <AppDialogTitle>
        {value.id ? "Edit scheduled task" : "Create scheduled task"}
      </AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: "var(--app-space-16)",
          }}
        >
          <AppTextField
            size="small"
            label="Name"
            value={value.name}
            onChange={(e) => set("name", e.target.value)}
          />
          <AppTextField
            size="small"
            label="Bash script"
            rows={8}
            multiline
            value={value.script}
            onChange={(e) => set("script", e.target.value)}
          />
          <AppSelect
            size="small"
            label="Run as"
            value={value.user}
            onChange={(e) => set("user", e.target.value)}
          >
            {(users.data ?? []).map((user) => (
              <option key={user.username} value={user.username}>
                {user.username}
              </option>
            ))}
          </AppSelect>
          <AppTextField
            size="small"
            label="Arguments (one per line)"
            rows={3}
            multiline
            value={argumentText}
            onChange={(e) => setArgumentText(e.target.value)}
          />
          <AppSelect
            label="Repeat"
            size="small"
            value={timing.frequency}
            onChange={(event) =>
              setTiming({
                ...timing,
                frequency: event.target.value as ScheduleTiming["frequency"],
                custom: calendar,
              })
            }
          >
            {Object.entries(frequencies).map(([key, label]) => (
              <option key={key} value={key}>
                {label}
              </option>
            ))}
          </AppSelect>
          {timing.frequency === "weekly" && (
            <AppSelect
              label="Day of the week"
              size="small"
              value={timing.weekday}
              onChange={(event) =>
                setTiming({
                  ...timing,
                  weekday: event.target.value as ScheduleTiming["weekday"],
                })
              }
            >
              {Object.entries(weekdays).map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
            </AppSelect>
          )}
          {(timing.frequency === "daily" || timing.frequency === "weekly") && (
            <AppTextField
              size="small"
              label="Time"
              type="time"
              shrinkLabel
              value={timing.time}
              onChange={(event) =>
                setTiming({ ...timing, time: event.target.value })
              }
            />
          )}
          {timing.frequency === "custom" && (
            <AppTextField
              size="small"
              label="Calendar expression"
              helperText="Advanced systemd calendar syntax. Daily and weekly schedules use the controls above."
              value={timing.custom}
              onChange={(event) =>
                setTiming({ ...timing, custom: event.target.value })
              }
            />
          )}
          {calendar && (
            <AppTypography color="text.secondary">
              {scheduleSummary(calendar)} ({value.timezone || "UTC"})
            </AppTypography>
          )}
          <AppTextField
            size="small"
            label="Timezone (defaults to UTC)"
            value={value.timezone}
            onChange={(e) => set("timezone", e.target.value)}
          />
          <AppTextField
            size="small"
            label="Timeout seconds"
            type="number"
            value={value.timeout_seconds}
            onChange={(e) => set("timeout_seconds", Number(e.target.value))}
          />
          <PathPickerField
            label="Working directory"
            browseLabel="Browse working directory"
            editable
            helperText="Optional. Leave empty to use the system default (/)."
            placeholder="/"
            value={value.working_directory}
            onChange={(path) => set("working_directory", path)}
          />
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={value.persistent}
                onChange={(e) => set("persistent", e.target.checked)}
              />
            }
            label="Run after a missed activation"
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={busy} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={
            busy ||
            !value.name.trim() ||
            !value.script.trim() ||
            !value.user ||
            !calendar.trim() ||
            value.timeout_seconds < 1
          }
          onClick={() =>
            onSave({
              ...value,
              on_calendar: calendar,
              arguments: argumentText.split("\n").filter((line) => line !== ""),
            })
          }
          variant="contained"
        >
          Save
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
}
