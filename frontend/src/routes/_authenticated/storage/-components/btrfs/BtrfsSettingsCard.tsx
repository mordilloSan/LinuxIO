import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio, useCallMutation, type BtrfsCompression } from "@/api";
import BtrfsMaintenanceControls from "@/components/cards/BtrfsMaintenanceControls";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import { useCapability } from "@/hooks/useCapabilities";

import { BTRFS_POOL_TOAST_META } from "./btrfsShared";

const ALGORITHMS = [
  { value: "none", label: "Off" },
  { value: "zstd", label: "zstd" },
  { value: "lzo", label: "lzo" },
  { value: "zlib", label: "zlib" },
];

const LEVELS: Record<string, { min: number; max: number; fallback: number }> = {
  zstd: { min: 1, max: 15, fallback: 3 },
  zlib: { min: 1, max: 9, fallback: 6 },
};

interface CompressionDraft {
  algorithm: string;
  level: string;
  force: boolean;
}

const draftFrom = (compression: BtrfsCompression): CompressionDraft => ({
  algorithm: compression.algorithm,
  level: String(
    compression.level ?? LEVELS[compression.algorithm]?.fallback ?? "",
  ),
  force: compression.force,
});

/** Compression is a filesystem-wide mount option: one control per filesystem. */
const CompressionControl = ({
  uuid,
  compression,
}: {
  uuid: string;
  compression: BtrfsCompression;
}) => {
  // Only held while editing; the current value comes from the query.
  const [edited, setEdited] = useState<CompressionDraft | null>(null);
  const draft = edited ?? draftFrom(compression);
  const range = LEVELS[draft.algorithm];
  const level = Number(draft.level);
  const levelValid =
    !range ||
    (Number.isInteger(level) && level >= range.min && level <= range.max);

  const { mutate: setCompression, isPending } = useCallMutation(
    linuxio.storage.btrfs_set_compression,
    {
      success: "Compression updated",
      error: "Failed to update compression",
      toast: BTRFS_POOL_TOAST_META,
    },
  );

  const apply = () =>
    setCompression(
      {
        uuid,
        algorithm: draft.algorithm,
        ...(range && { level }),
        force: draft.force,
      },
      { onSuccess: () => setEdited(null) },
    );

  return (
    <div
      style={{
        alignItems: "center",
        display: "flex",
        flexWrap: "wrap",
        gap: "var(--app-space-12)",
        padding: "var(--app-space-8) 0",
      }}
    >
      <AppSelect
        label="Compression"
        onChange={(event) => {
          const algorithm = event.target.value;
          setEdited({
            ...draft,
            algorithm,
            level: String(LEVELS[algorithm]?.fallback ?? ""),
          });
        }}
        value={draft.algorithm}
      >
        {ALGORITHMS.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </AppSelect>
      {range && (
        <AppTextField
          error={!levelValid}
          helperText={`${range.min}-${range.max}`}
          label="Level"
          onChange={(event) =>
            setEdited({ ...draft, level: event.target.value })
          }
          size="small"
          type="number"
          value={draft.level}
        />
      )}
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={draft.force}
            onChange={(_, force) => setEdited({ ...draft, force })}
          />
        }
        label={
          <>
            Force
            <AppTypography
              color="text.secondary"
              style={{ display: "block" }}
              variant="caption"
            >
              Compress even data that looks incompressible
            </AppTypography>
          </>
        }
      />
      <AppButton
        disabled={isPending || !levelValid}
        onClick={apply}
        size="small"
        variant="outlined"
      >
        Apply
      </AppButton>
      <AppChip
        color={compression.fstabPersisted ? "success" : "default"}
        label={
          compression.fstabPersisted ? "Persisted in fstab" : "Not in fstab"
        }
        size="small"
        variant="soft"
      />
    </div>
  );
};

interface BtrfsSettingsCardProps {
  uuid: string;
  /** Scrub/balance schedules are keyed by mountpoint; absent when unmounted. */
  mountpoint?: string;
}

const BtrfsSettingsCard = ({ uuid, mountpoint }: BtrfsSettingsCardProps) => {
  const { data: settings, error } = useQuery(
    linuxio.storage.btrfs_settings({ uuid }),
  );
  const { mutate: setQuotas, isPending: isSettingQuotas } = useCallMutation(
    linuxio.storage.btrfs_set_quotas,
    {
      success: "Quota setting updated",
      error: "Failed to update quotas",
      toast: BTRFS_POOL_TOAST_META,
    },
  );

  const { isEnabled, reason } = useCapability("btrfsMaintenanceAvailable");
  const { data: schedule } = useQuery({
    ...linuxio.storage.get_btrfs_maintenance,
    enabled: isEnabled,
  });
  const { mutate: setMaintenance, isPending: isUpdatingMaintenance } =
    useCallMutation(linuxio.storage.set_btrfs_maintenance, {
      success: "Btrfs maintenance schedule updated",
      error: "Failed to update btrfs maintenance schedule",
      toast: BTRFS_POOL_TOAST_META,
    });

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          iconTint
          icon={<Icon height={24} icon="mdi:cog-outline" width={24} />}
          subtitle="Quotas, compression and maintenance timers"
          title="Settings"
        />
        {error && <AppAlert severity="error">{error.message}</AppAlert>}
        {settings && (
          <>
            <InfoRow label="Quotas">
              <AppFormControlLabel
                control={
                  <AppSwitch
                    aria-label="Quotas"
                    checked={settings.quotasEnabled}
                    disabled={isSettingQuotas}
                    onChange={(_, enabled) => setQuotas({ uuid, enabled })}
                  />
                }
                label={
                  <AppTypography color="text.secondary" variant="caption">
                    Enables per-subvolume usage and limits. Adds some write
                    overhead.
                  </AppTypography>
                }
              />
            </InfoRow>
            <CompressionControl
              compression={settings.compression}
              uuid={uuid}
            />
          </>
        )}
        {mountpoint && (
          <BtrfsMaintenanceControls
            btrfsMaintenance={schedule ?? null}
            btrfsMaintenanceReason={isEnabled ? null : reason}
            isUpdatingMaintenance={isUpdatingMaintenance}
            mountpoint={mountpoint}
            onSetMaintenance={(target, task, period) =>
              setMaintenance({ mountpoint: target, task, period })
            }
          />
        )}
      </AppCardContent>
    </FrostedCard>
  );
};

export default BtrfsSettingsCard;
