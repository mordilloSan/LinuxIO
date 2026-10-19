import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  useCallMutation,
  type BtrfsKeep,
  type BtrfsSnapshotRule,
  type BtrfsSubvolume,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";

import { BTRFS_TOAST_META } from "./btrfsShared";

const EVERY_OPTIONS = [
  { value: "hourly", label: "Hourly", summary: "Every hour" },
  { value: "daily", label: "Daily", summary: "Every day" },
  { value: "weekly", label: "Weekly", summary: "Every week" },
] as const;

const KEEP_FIELDS = [
  { key: "hourly", label: "Keep hourly", unit: "h" },
  { key: "daily", label: "Keep daily", unit: "d" },
  { key: "weekly", label: "Keep weekly", unit: "w" },
  { key: "monthly", label: "Keep monthly", unit: "m" },
] as const satisfies readonly {
  key: keyof BtrfsKeep;
  label: string;
  unit: string;
}[];

const keepSummary = (keep: BtrfsKeep) =>
  KEEP_FIELDS.map(({ key, unit }) => `${keep[key]}${unit}`).join(" · ");

const scheduleSummary = (every: string) =>
  EVERY_OPTIONS.find((option) => option.value === every)?.summary ?? every;

const BtrfsSnapshotRulesCard = ({ uuid }: { uuid: string }) => {
  const toast = useScopedToast(BTRFS_TOAST_META);
  // Lazy: the route loader cannot know the selected filesystem's uuid.
  const {
    data: subvolumeData = [],
    error: subvolumesError,
    isPending,
  } = useQuery(linuxio.storage.btrfs_subvolumes({ uuid }));
  const { data: rulesData, error: rulesError } = useQuery(
    linuxio.storage.btrfs_snapshot_rules,
  );
  const [draft, setDraft] = useState<BtrfsSnapshotRule | null>(null);

  const { mutate: saveRule, isPending: isSaving } = useCallMutation(
    linuxio.storage.btrfs_snapshot_rule_set,
    {
      success: "Snapshot rule saved",
      error: "Failed to save snapshot rule",
      toast: BTRFS_TOAST_META,
    },
  );
  const { mutate: removeRule, isPending: isRemoving } = useCallMutation(
    linuxio.storage.btrfs_snapshot_rule_delete,
    {
      success: "Snapshot rule removed",
      error: "Failed to remove snapshot rule",
      toast: BTRFS_TOAST_META,
    },
  );
  const { mutate: runNow, isPending: isRunning } = useCallMutation(
    linuxio.storage.btrfs_snapshot_run_now,
    {
      success: (report) => {
        toast.success(
          `Snapshots run: ${report.taken.length} taken, ${report.pruned.length} pruned`,
        );
        if (report.errors.length > 0) toast.error(report.errors[0]);
      },
      error: "Failed to run snapshots",
      toast: BTRFS_TOAST_META,
    },
  );

  const subvolumes = subvolumeData.filter((subvolume) => !subvolume.isSnapshot);
  const rules = (rulesData?.rules ?? []).filter((rule) => rule.uuid === uuid);
  const ruleFor = (path: string) => rules.find((rule) => rule.path === path);
  const hasRule = draft !== null && ruleFor(draft.path) !== undefined;
  const keepsNothing =
    draft !== null &&
    draft.enabled &&
    KEEP_FIELDS.every(({ key }) => draft.keep[key] === 0);

  const edit = (subvolume: BtrfsSubvolume) =>
    setDraft(
      ruleFor(subvolume.path) ?? {
        uuid,
        path: subvolume.path,
        enabled: false,
        every: "hourly",
        keep: { hourly: 24, daily: 7, weekly: 4, monthly: 6 },
        beforeUpdates: false,
      },
    );
  const closeDialog = () => setDraft(null);
  const handleSave = () => {
    if (!draft) return;
    saveRule({ rule: draft }, { onSuccess: closeDialog });
  };
  const handleRemove = () => {
    if (!draft) return;
    removeRule(
      { uuid: draft.uuid, path: draft.path },
      { onSuccess: closeDialog },
    );
  };

  const columns: AppVirtualTableColumnDef<BtrfsSubvolume>[] = [
    { accessorKey: "path", header: "Path" },
    {
      id: "schedule",
      header: "Schedule",
      cell: ({ row }) => {
        const rule = ruleFor(row.original.path);
        return rule?.enabled ? (
          scheduleSummary(rule.every)
        ) : (
          <AppChip label="Off" size="small" variant="soft" />
        );
      },
    },
    {
      id: "keep",
      header: "Keep",
      cell: ({ row }) => {
        const rule = ruleFor(row.original.path);
        return rule ? keepSummary(rule.keep) : "-";
      },
    },
    {
      id: "beforeUpdates",
      header: "Before updates",
      cell: ({ row }) => (
        <AppChip
          label={ruleFor(row.original.path)?.beforeUpdates ? "Yes" : "No"}
          size="small"
          variant="soft"
        />
      ),
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => (
        <AppButton
          onClick={() => edit(row.original)}
          size="small"
          variant="outlined"
        >
          Edit
        </AppButton>
      ),
      meta: { align: "right" },
    },
  ];

  const error = subvolumesError ?? rulesError;

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          icon={<Icon height={24} icon="mdi:camera-timer" width={24} />}
          right={
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              {rulesData && (
                <AppChip
                  color={rulesData.timerEnabled ? "success" : "default"}
                  label={
                    rulesData.timerEnabled ? "Timer enabled" : "Timer disabled"
                  }
                  size="small"
                  variant="soft"
                />
              )}
              <AppButton
                disabled={isRunning}
                onClick={() => runNow()}
                size="small"
                variant="outlined"
              >
                Run now
              </AppButton>
            </div>
          }
          subtitle="Timeline snapshots per subvolume"
          title="Automatic snapshots"
        />
        {error && <AppAlert severity="error">{error.message}</AppAlert>}
        <AppVirtualTable
          ariaLabel="Btrfs snapshot rules"
          columns={columns}
          data={subvolumes}
          density="compact"
          emptyMessage={isPending ? "Loading..." : "No subvolumes found"}
          fillAvailable={false}
          getRowId={(subvolume) => String(subvolume.id)}
          maxHeight={320}
          variant="embedded"
        />
      </AppCardContent>

      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={closeDialog}
        open={draft !== null}
      >
        <AppDialogTitle>Automatic snapshots for {draft?.path}</AppDialogTitle>
        {draft && (
          <AppDialogContent>
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                gap: "var(--app-space-4)",
              }}
            >
              <AppFormControlLabel
                control={
                  <AppSwitch
                    checked={draft.enabled}
                    onChange={(_, enabled) => setDraft({ ...draft, enabled })}
                  />
                }
                label="Enabled"
              />
              <AppSelect
                fullWidth
                label="Every"
                onChange={(event) =>
                  setDraft({ ...draft, every: event.target.value })
                }
                value={draft.every}
              >
                {EVERY_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </AppSelect>
              {KEEP_FIELDS.map(({ key, label }) => (
                <AppTextField
                  fullWidth
                  key={key}
                  label={label}
                  onChange={(event) =>
                    setDraft({
                      ...draft,
                      keep: {
                        ...draft.keep,
                        [key]: Math.max(
                          0,
                          Math.trunc(Number(event.target.value)) || 0,
                        ),
                      },
                    })
                  }
                  size="small"
                  type="number"
                  value={draft.keep[key]}
                />
              ))}
              <AppTypography
                color={keepsNothing ? "error" : "text.secondary"}
                variant="caption"
              >
                0 = keep none of this bucket; an enabled rule needs at least one
              </AppTypography>
              <AppFormControlLabel
                control={
                  <AppSwitch
                    checked={draft.beforeUpdates}
                    onChange={(_, beforeUpdates) =>
                      setDraft({ ...draft, beforeUpdates })
                    }
                  />
                }
                label={
                  <>
                    Snapshot before updates
                    <AppTypography
                      color="text.secondary"
                      style={{ display: "block" }}
                      variant="caption"
                    >
                      Taken by LinuxIO updates and by the distro auto-update
                      unit when its LinuxIO notification policy is not Never.
                    </AppTypography>
                  </>
                }
              />
            </div>
          </AppDialogContent>
        )}
        <AppDialogActions>
          <AppButton disabled={isSaving || isRemoving} onClick={closeDialog}>
            Cancel
          </AppButton>
          {hasRule && (
            <AppButton
              color="error"
              disabled={isSaving || isRemoving}
              onClick={handleRemove}
            >
              Remove rule
            </AppButton>
          )}
          <AppButton
            disabled={isSaving || isRemoving || keepsNothing}
            onClick={handleSave}
            variant="contained"
          >
            {isSaving ? "Saving..." : "Save"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </FrostedCard>
  );
};

export default BtrfsSnapshotRulesCard;
