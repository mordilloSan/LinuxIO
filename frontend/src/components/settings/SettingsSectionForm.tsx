import { Icon } from "@iconify/react";
import { useMemo, useState, type ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";

/**
 * Draft-form state shared by the settings sections (Indexer, Monitoring,
 * Docker folders, Docker auto-update): a sparse patch over the saved config,
 * value-based dirty tracking, and the restart-required flag. Domain logic —
 * toDraft, toPatchPayload, validateDraft — stays in each section. `isEqual`
 * overrides the JSON comparison when the saved shape has a canonical form
 * (normalised paths, order-insensitive lists).
 */
export function useSettingsDraft<TDraft extends object, TErrors extends object>(
  savedDraft: TDraft | null,
  mergeDraft?: (saved: TDraft, patch: Partial<TDraft>) => TDraft,
  isEqual?: (draft: TDraft, saved: TDraft) => boolean,
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

  const isDirty =
    isEqual && draft && savedDraft
      ? !isEqual(draft, savedDraft)
      : JSON.stringify(draft) !== JSON.stringify(savedDraft);

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
export const SettingsSectionShell = ({
  title,
  subtitle,
  refreshAriaLabel,
  refreshing,
  refreshDisabled,
  onRefresh,
  children,
}: {
  title: string;
  subtitle: string;
  refreshAriaLabel: string;
  refreshing: boolean;
  refreshDisabled?: boolean;
  onRefresh: () => void;
  children: ReactNode;
}) => {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-6)",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: "var(--app-space-6)",
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
export const SettingsGrid = ({
  children,
  minColumnWidth = 220,
  rowGap = "var(--app-space-6)",
}: {
  children: ReactNode;
  minColumnWidth?: number;
  rowGap?: string;
}) => {
  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(auto-fit, minmax(${minColumnWidth}px, 1fr))`,
        columnGap: "var(--app-space-6)",
        rowGap,
      }}
    >
      {children}
    </div>
  );
};

/** Reset/Save button row for draft-form sections. */
export const SettingsSaveFooter = ({
  isDirty,
  busy,
  saveDisabled,
  saving,
  onReset,
  onSave,
}: {
  isDirty: boolean;
  busy: boolean;
  saveDisabled?: boolean;
  saving: boolean;
  onReset: () => void;
  onSave: () => void;
}) => {
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "flex-end",
        gap: "var(--app-space-6)",
        paddingTop: "var(--app-space-2)",
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
