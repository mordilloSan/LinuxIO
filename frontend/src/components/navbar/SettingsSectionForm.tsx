import { Icon } from "@iconify/react";
import React, { useMemo, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

/**
 * Draft-form state shared by the daemon settings sections (Indexer,
 * Monitoring): a sparse patch over the saved config, value-based dirty
 * tracking, and the restart-required flag. Domain logic — toDraft,
 * toPatchPayload, validateDraft — stays in each section.
 */
export function useSettingsDraft<TDraft extends object, TErrors extends object>(
  savedDraft: TDraft | null,
  mergeDraft?: (saved: TDraft, patch: Partial<TDraft>) => TDraft,
) {
  const [draftPatch, setDraftPatch] = useState<Partial<TDraft>>({});
  const [errors, setErrors] = useState<TErrors>({} as TErrors);
  const [restartRequired, setRestartRequired] = useState(false);

  const draft = useMemo(() => {
    if (!savedDraft) return null;
    return mergeDraft
      ? mergeDraft(savedDraft, draftPatch)
      : { ...savedDraft, ...draftPatch };
  }, [draftPatch, mergeDraft, savedDraft]);

  const isDirty = JSON.stringify(draft) !== JSON.stringify(savedDraft);

  /** Set one draft key; a value equal to the saved one clears the entry. */
  const patchKey = <K extends keyof TDraft>(key: K, value: TDraft[K]) => {
    setDraftPatch((prev) => {
      if (!savedDraft) return prev;
      if (Object.is(savedDraft[key], value)) {
        const next = { ...prev };
        delete next[key];
        return next;
      }
      return { ...prev, [key]: value };
    });
    setRestartRequired(false);
  };

  const reset = () => {
    setDraftPatch({});
    setErrors({} as TErrors);
    setRestartRequired(false);
  };

  return {
    draft,
    draftPatch,
    setDraftPatch,
    errors,
    setErrors,
    restartRequired,
    setRestartRequired,
    isDirty,
    patchKey,
    reset,
  };
}

/** Section column with the title/subtitle header and refresh button. */
export const SettingsSectionShell: React.FC<{
  title: string;
  subtitle: string;
  refreshAriaLabel: string;
  refreshing: boolean;
  refreshDisabled?: boolean;
  onRefresh: () => void;
  children: React.ReactNode;
}> = ({
  title,
  subtitle,
  refreshAriaLabel,
  refreshing,
  refreshDisabled,
  onRefresh,
  children,
}) => {
  const theme = useAppTheme();
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(1.5),
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: theme.spacing(1.5),
        }}
      >
        <div>
          <AppTypography fontWeight={600} variant="body1">
            {title}
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            {subtitle}
          </AppTypography>
        </div>
        <AppTooltip title={refreshing ? "Refreshing" : "Refresh"}>
          <AppIconButton
            aria-label={refreshAriaLabel}
            disabled={refreshing || refreshDisabled}
            onClick={onRefresh}
            size="small"
          >
            <Icon
              height={18}
              icon={refreshing ? "mdi:loading" : "mdi:refresh"}
              width={18}
            />
          </AppIconButton>
        </AppTooltip>
      </div>
      {children}
    </div>
  );
};

/** Responsive auto-fit form grid used inside SectionCards. */
export const SettingsGrid: React.FC<{
  children: React.ReactNode;
  minColumnWidth?: number;
  rowGap?: number;
}> = ({ children, minColumnWidth = 220, rowGap = 1.5 }) => {
  const theme = useAppTheme();
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(auto-fit, minmax(${minColumnWidth}px, 1fr))`,
        columnGap: theme.spacing(1.5),
        rowGap: theme.spacing(rowGap),
      }}
    >
      {children}
    </div>
  );
};

/** Reset/Save button row for draft-form sections. */
export const SettingsSaveFooter: React.FC<{
  isDirty: boolean;
  busy: boolean;
  saveDisabled?: boolean;
  saving: boolean;
  onReset: () => void;
  onSave: () => void;
}> = ({ isDirty, busy, saveDisabled, saving, onReset, onSave }) => {
  const theme = useAppTheme();
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "flex-end",
        gap: theme.spacing(1.5),
        paddingTop: theme.spacing(0.5),
      }}
    >
      <AppButton disabled={!isDirty || busy} onClick={onReset}>
        Reset
      </AppButton>
      <AppButton
        disabled={!isDirty || busy || saveDisabled}
        onClick={onSave}
        startIcon={
          <Icon height={18} icon="mdi:content-save-outline" width={18} />
        }
        variant="contained"
      >
        {saving ? "Saving..." : "Save"}
      </AppButton>
    </div>
  );
};
