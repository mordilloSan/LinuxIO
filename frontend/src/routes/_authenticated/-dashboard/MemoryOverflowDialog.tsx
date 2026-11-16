import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useEffectEvent, useId, useRef, useState } from "react";

import "./memory-overflow.css";

import { linuxio, type SwapStatus } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppRadio from "@/components/ui/AppRadio";
import AppSlider from "@/components/ui/AppSlider";
import AppTypography from "@/components/ui/AppTypography";

import {
  bannerView,
  canApply,
  initialSelection,
  modeResultWarning,
  needsSwapFileConfirm,
  presetSwappiness,
  rowName,
  rowViews,
  type BannerAction,
  type RowMode,
  type RowView,
} from "./memoryStatusView";

const CAPABILITIES_LINK_TEXT = "Open Capabilities to install zram";
const DASHBOARD_TOAST_META = { label: "Open dashboard", to: "/" } as const;

/** How long a key press on the eagerness slider waits for the next one. */
const KEY_SAVE_DELAY_MS = 500;

/** The keys that move a range input. */
const SLIDER_KEYS = new Set([
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "ArrowUp",
  "End",
  "Home",
  "PageDown",
  "PageUp",
]);

interface Props {
  onClose: () => void;
  open: boolean;
}

const CapabilitiesLink = () => (
  <Link search={{ tab: "capabilities" }} to="/settings">
    {CAPABILITIES_LINK_TEXT}
  </Link>
);

const ModeRow = ({
  checked,
  disabled,
  groupName,
  onSelect,
  row,
}: {
  checked: boolean;
  disabled: boolean;
  groupName: string;
  onSelect: (mode: RowMode) => void;
  row: RowView;
}) => {
  const id = useId();
  const describedBy = [`${id}-chips`, `${id}-description`, `${id}-note`].join(
    " ",
  );
  const unavailable = row.disabledReason !== undefined;
  // Name, caption and description are one label, so clicking any of them
  // selects the row. The caution, the reason and the Capabilities link stay
  // outside it: a link must not sit inside a label.
  const text = (
    <>
      <span className="memory-row__title">
        <span className="memory-overflow__name" id={`${id}-name`}>
          {row.name}
        </span>
        <span className="memory-row__chips" id={`${id}-chips`}>
          {row.recommended ? (
            <AppChip
              color="primary"
              label="Recommended"
              size="small"
              variant="soft"
            />
          ) : null}
          {row.current ? (
            <AppChip
              color="default"
              label="Current"
              size="small"
              variant="outlined"
            />
          ) : null}
        </span>
      </span>
      <AppTypography color="text.secondary" component="span" variant="caption">
        {row.technical}
      </AppTypography>
      <AppTypography component="span" id={`${id}-description`} variant="body2">
        {row.description}
      </AppTypography>
    </>
  );
  return (
    <div
      className={
        unavailable || row.capabilitiesLink
          ? "memory-row memory-row--unavailable"
          : "memory-row"
      }
    >
      <div className="memory-row__control">
        {row.capabilitiesLink ? null : (
          <AppRadio
            aria-describedby={describedBy}
            aria-labelledby={`${id}-name`}
            checked={checked}
            disabled={disabled || unavailable}
            id={id}
            name={groupName}
            onChange={() => onSelect(row.mode)}
            value={row.mode}
          />
        )}
      </div>
      <div className="memory-row__body">
        {row.capabilitiesLink ? (
          <div className="memory-row__text">{text}</div>
        ) : (
          <label className="memory-row__text" htmlFor={id}>
            {text}
          </label>
        )}
        <div className="memory-row__note" id={`${id}-note`}>
          {row.caution ? (
            <AppTypography color="warning" variant="caption">
              {row.caution}
            </AppTypography>
          ) : null}
          {row.disabledReason ? (
            <AppTypography color="text.secondary" variant="caption">
              {row.disabledReason}
            </AppTypography>
          ) : null}
          {row.capabilitiesLink ? (
            <AppTypography variant="caption">
              <CapabilitiesLink />
            </AppTypography>
          ) : null}
        </div>
      </div>
    </div>
  );
};

/**
 * What the host does when memory runs out, opened from the dashboard's Memory
 * card. It stays mounted while closed, so it reads the status only while open
 * and drops its choices when it closes.
 */
const MemoryOverflowDialog = ({ open, onClose }: Props) => {
  const groupName = useId();
  const groupLabelId = useId();
  const advancedId = useId();
  const [selected, setSelected] = useState<RowMode | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [focusApply, setFocusApply] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [draftSwappiness, setDraftSwappiness] = useState<number | null>(null);
  const [progressMessage, setProgressMessage] = useState<string | null>(null);
  // The newest value the slider reported: a save's own callback runs with the
  // render it started in, so it cannot read the draft state.
  const newestStep = useRef<number | null>(null);
  // Counts the key releases on the slider whose step waits to be saved; null
  // when none waits.
  const [keyWait, setKeyWait] = useState<number | null>(null);
  const {
    data: status,
    isPending,
    error,
  } = useQuery({
    ...linuxio.memory.get_swap_status,
    enabled: open,
    refetchInterval: 15000,
  });

  // A task, not a call: stopping a swap file moves its data back first.
  const setMode = linuxio.memory.set_swap_mode.useMutation({
    onProgress: (progress) => setProgressMessage(progress.message ?? null),
    success: "Memory overflow setting applied",
    warning: (result, request) => modeResultWarning(result, request.mode),
    error: "Failed to change how memory runs out",
    toast: DASHBOARD_TOAST_META,
    options: {
      onSettled: () => {
        setProgressMessage(null);
        setConfirming(false);
      },
    },
  });
  const drain = linuxio.memory.drain_swap.useMutation({
    onProgress: (progress) => setProgressMessage(progress.message ?? null),
    success: "Parked data brought back to memory",
    error: "Failed to bring parked data back to memory",
    toast: DASHBOARD_TOAST_META,
    options: { onSettled: () => setProgressMessage(null) },
  });
  const setSwappiness = linuxio.memory.set_swappiness.useMutation({
    success: "Eagerness updated",
    error: "Failed to change the eagerness",
    toast: DASHBOARD_TOAST_META,
    options: {
      // A step made while the save ran is not lost: only the saved value's
      // draft is dropped, so the slider then follows the host.
      onSettled: (_status, _error, request) =>
        setDraftSwappiness((draft) =>
          draft === request.swappiness ? null : draft,
        ),
    },
  });
  // The slider stays enabled while its own save runs: disabling the range
  // input the user is holding would drop keyboard focus.
  const otherChangeRunning = setMode.isPending || drain.isPending;
  const busy = otherChangeRunning || setSwappiness.isPending;

  const stepSwappiness = (value: number) => {
    newestStep.current = value;
    setDraftSwappiness(value);
  };

  const saveSwappiness = (value: number) =>
    setSwappiness.mutate(
      { swappiness: value },
      {
        // A step made while this save ran is sent once it succeeded, so the
        // slider never shows a value the host does not have.
        onSuccess: () => {
          const newest = newestStep.current;
          if (newest !== null && newest !== value) saveSwappiness(newest);
        },
        // After a failed save nothing is retried and the slider goes back to
        // the host's value, a step made meanwhile included.
        onError: () => {
          newestStep.current = null;
          setDraftSwappiness(null);
        },
      },
    );

  const commitSwappiness = () => {
    setKeyWait(null);
    if (draftSwappiness === null || !status) return;
    if (otherChangeRunning) {
      // A mode change writes its own value and the host refuses a second
      // change meanwhile: the step is dropped.
      newestStep.current = null;
      setDraftSwappiness(null);
      return;
    }
    // One save at a time; the draft stays and goes out when the save ends.
    if (setSwappiness.isPending) return;
    if (draftSwappiness === status.swappiness) {
      // Dragged back to the host's value: nothing to save, and the slider
      // follows the host again.
      setDraftSwappiness(null);
      return;
    }
    saveSwappiness(draftSwappiness);
  };

  // A run of arrow keys is one save and one toast: each release restarts the
  // wait, and the commit that ends it sees the dialog as it is then.
  const commitAfterKeyWait = useEffectEvent(commitSwappiness);
  useEffect(() => {
    if (keyWait === null) return;
    const timer = window.setTimeout(commitAfterKeyWait, KEY_SAVE_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [keyWait]);
  const commitSwappinessAfterKeys = () =>
    setKeyWait((releases) => (releases ?? 0) + 1);

  // A running change holds the dialog open: its progress and its result are
  // shown here, and the host refuses a second change meanwhile.
  const handleClose = () => {
    if (busy) return;
    // A key press still waiting is saved, not dropped.
    if (keyWait !== null) commitSwappiness();
    setSelected(null);
    setConfirming(false);
    setFocusApply(false);
    setAdvancedOpen(false);
    setDraftSwappiness(null);
    newestStep.current = null;
    onClose();
  };

  const dialogProps = {
    "aria-busy": busy || undefined,
    disableEscapeKeyDown: busy,
    fullWidth: true,
    maxWidth: "sm",
    onClose: handleClose,
    open,
  } as const;
  // Keyed so the button, which has focus while the status loads, is the same
  // element once the rows and Apply appear beside it.
  const closeButton = (
    <AppButton disabled={busy} key="close" onClick={handleClose}>
      Close
    </AppButton>
  );

  // A refresh that fails while there is a status keeps showing it.
  if (!status) {
    return (
      <GeneralDialog {...dialogProps}>
        <AppDialogTitle>Memory overflow</AppDialogTitle>
        <AppDialogContent>
          {isPending ? (
            <div aria-busy className="memory-overflow__loading">
              <ComponentLoader />
            </div>
          ) : (
            <AppAlert severity="error">
              <AppAlertTitle>Memory status unavailable</AppAlertTitle>
              {error?.message || "LinuxIO could not read the memory status."}
            </AppAlert>
          )}
        </AppDialogContent>
        <AppDialogActions>{closeButton}</AppDialogActions>
      </GeneralDialog>
    );
  }

  const choice = selected ?? initialSelection(status);
  const rows = rowViews(status);
  const banner = bannerView(status);

  // Turning zram on while the swap file is active also stops the file, so it
  // asks first, as a second step of this dialog; every other change applies
  // at once.
  const requestMode = (mode: RowMode) => {
    setSelected(mode);
    if (needsSwapFileConfirm(status, mode)) {
      setFocusApply(false);
      setConfirming(true);
    } else {
      setMode.mutate({ mode });
    }
  };

  if (confirming) {
    return (
      <GeneralDialog {...dialogProps}>
        <AppDialogTitle>Switch to compressed memory?</AppDialogTitle>
        <AppDialogContent>
          <ul className="memory-overflow__steps">
            <li>Compressed memory is turned on.</li>
            <li>Data parked in the swap file is brought back to memory.</li>
            <li>The swap file is turned off and stays off after a restart.</li>
            <li>The file itself is not deleted.</li>
          </ul>
          <AppTypography
            aria-live="polite"
            color="text.secondary"
            role="status"
            variant="caption"
          >
            {setMode.isPending ? (progressMessage ?? "Starting…") : ""}
          </AppTypography>
        </AppDialogContent>
        <AppDialogActions>
          {/* Focus lands on the way back, not on the change. */}
          <AppButton
            autoFocus
            disabled={busy}
            onClick={() => {
              setConfirming(false);
              setFocusApply(true);
            }}
          >
            Back
          </AppButton>
          <AppButton
            disabled={busy}
            onClick={() => setMode.mutate({ mode: "zram" })}
            variant="contained"
          >
            {setMode.isPending ? "Applying…" : "Turn on compressed memory"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    );
  }

  const recommended = presetSwappiness(status, choice);
  const swappiness = draftSwappiness ?? status.swappiness;

  return (
    <GeneralDialog {...dialogProps}>
      <AppDialogTitle>Memory overflow</AppDialogTitle>
      <AppDialogContent>
        <div className="memory-overflow">
          <AppAlert
            action={
              banner.action.kind === "none" ? undefined : (
                <BannerActionControl
                  action={banner.action}
                  busy={busy}
                  drainPending={drain.isPending}
                  onDrain={() => drain.mutate()}
                  onSwitchToZram={() => requestMode("zram")}
                />
              )
            }
            role="status"
            severity={banner.severity}
          >
            {banner.text}
            {banner.action.kind === "drain" && banner.action.blockedReason ? (
              <div className="memory-overflow__hint">
                {banner.action.blockedReason}
              </div>
            ) : null}
          </AppAlert>

          <div className="memory-overflow__group">
            <span className="memory-overflow__name" id={groupLabelId}>
              When memory runs out
            </span>
            <div
              aria-labelledby={groupLabelId}
              className="memory-overflow__rows"
              role="radiogroup"
            >
              {rows.map((row) => (
                <ModeRow
                  checked={choice === row.mode}
                  disabled={busy}
                  groupName={groupName}
                  key={row.mode}
                  onSelect={setSelected}
                  row={row}
                />
              ))}
            </div>
          </div>

          <div className="memory-overflow__group">
            <AppButton
              aria-controls={advancedId}
              aria-expanded={advancedOpen}
              className="memory-overflow__advanced"
              color="inherit"
              keepTextOnMobile
              onClick={() => setAdvancedOpen((isOpen) => !isOpen)}
              size="small"
              startIcon={
                <Icon
                  aria-hidden="true"
                  height={18}
                  icon={advancedOpen ? "mdi:chevron-up" : "mdi:chevron-down"}
                  width={18}
                />
              }
            >
              Advanced
            </AppButton>
            <div id={advancedId}>
              <AppCollapse in={advancedOpen} unmountOnExit>
                <SwappinessControl
                  disabled={otherChangeRunning || status.tuned_active}
                  onChange={stepSwappiness}
                  onCommit={commitSwappiness}
                  onKeyCommit={commitSwappinessAfterKeys}
                  recommended={recommended}
                  recommendedFor={rowName(choice)}
                  status={status}
                  value={swappiness}
                />
              </AppCollapse>
            </div>
          </div>
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppTypography
          aria-live="polite"
          className="memory-overflow__progress"
          color="text.secondary"
          role="status"
          variant="caption"
        >
          {busy && progressMessage ? progressMessage : ""}
        </AppTypography>
        {closeButton}
        <AppButton
          autoFocus={focusApply}
          disabled={busy || !canApply(status, choice)}
          onClick={() => requestMode(choice)}
          variant="contained"
        >
          {setMode.isPending ? "Applying…" : "Apply"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

const SwappinessControl = ({
  disabled,
  onChange,
  onCommit,
  onKeyCommit,
  recommended,
  recommendedFor,
  status,
  value,
}: {
  disabled: boolean;
  onChange: (value: number) => void;
  /** The pointer was released or focus left the slider. */
  onCommit: () => void;
  /** A key that moves the slider was released. */
  onKeyCommit: () => void;
  recommended: number | undefined;
  recommendedFor: string;
  status: SwapStatus;
  value: number;
}) => {
  const id = useId();
  return (
    <div className="memory-overflow__slider">
      <div className="memory-overflow__slider-label">
        <label className="memory-overflow__name" htmlFor={id}>
          Eagerness to move data out of memory
        </label>
        <AppTypography variant="body2">{value}</AppTypography>
      </div>
      <AppSlider
        aria-describedby={`${id}-note`}
        disabled={disabled}
        id={id}
        max={200}
        min={0}
        onBlur={onCommit}
        onChange={onChange}
        onKeyUp={(event) => {
          if (SLIDER_KEYS.has(event.key)) onKeyCommit();
        }}
        onPointerUp={onCommit}
        value={value}
      />
      <div id={`${id}-note`}>
        {status.tuned_active ? (
          <AppTypography color="text.secondary" variant="caption">
            {`Managed by the TuneD profile ${status.tuned_profile}`}
          </AppTypography>
        ) : recommended !== undefined ? (
          <AppTypography color="text.secondary" variant="caption">
            {`Recommended for ${recommendedFor}: ${recommended}`}
          </AppTypography>
        ) : null}
      </div>
    </div>
  );
};

const BannerActionControl = ({
  action,
  busy,
  drainPending,
  onDrain,
  onSwitchToZram,
}: {
  action: BannerAction;
  busy: boolean;
  drainPending: boolean;
  onDrain: () => void;
  onSwitchToZram: () => void;
}) => {
  switch (action.kind) {
    case "enable_zram":
      return (
        <AppButton disabled={busy} onClick={onSwitchToZram} size="small">
          Turn on compressed memory
        </AppButton>
      );
    case "switch_zram":
      return (
        <AppButton disabled={busy} onClick={onSwitchToZram} size="small">
          Switch to compressed memory
        </AppButton>
      );
    case "capabilities":
      return <CapabilitiesLink />;
    case "drain":
      return (
        <AppButton
          disabled={busy || !action.canDrain}
          onClick={onDrain}
          size="small"
        >
          {drainPending ? "Bringing back…" : "Bring back to memory"}
        </AppButton>
      );
    case "none":
      return null;
  }
};

export default MemoryOverflowDialog;
