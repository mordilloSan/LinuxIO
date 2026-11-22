import { Icon } from "@iconify/react";
import { useIsMutating, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useEffectEvent, useId, useRef, useState } from "react";
import { toast } from "sonner";

import "./memory-overflow.css";

import {
  actionErrorMessage,
  isTaskCancellationError,
  type LinuxIOError,
  linuxio,
  type SwapMode,
  type SwapModeRequest,
  type SwapStatus,
  type TaskSnapshot,
} from "@/api";
import { toastLinkOptions } from "@/api/toast-meta";
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
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import {
  MEMORY_DRAIN_FAILED,
  MEMORY_DRAINED,
  MEMORY_MODE_APPLIED,
  MEMORY_MODE_FAILED,
  MEMORY_TOAST_META,
  modeResultWarning,
  type SwapFileAction,
  swapFileDone,
  swapProgressText,
} from "@/tasks/memoryFeedback";
import {
  TASK_TYPE_MEMORY_CREATE_SWAP_FILE,
  TASK_TYPE_MEMORY_DRAIN,
  TASK_TYPE_MEMORY_MOVE_SWAP_FILE,
  TASK_TYPE_MEMORY_REMOVE_SWAP_FILE,
  TASK_TYPE_MEMORY_RESIZE_SWAP_FILE,
  TASK_TYPE_MEMORY_SWAP_MODE,
} from "@/tasks/taskTypes";
import { requestString, taskMetadataObject } from "@/tasks/taskUtils";
import { markTerminalFeedbackEmitted } from "@/tasks/terminalTaskFeedback";
import { useActiveTaskRecovery } from "@/tasks/useActiveTaskRecovery";

import {
  bannerView,
  canApply,
  checkSize,
  confirmView,
  createView,
  generatorFilesNote,
  initialSelection,
  isRowMode,
  otherSwapViews,
  presetSwappiness,
  rowName,
  rowViews,
  swapFileViews,
  type BannerAction,
  type ConfirmView,
  type RowMode,
  type RowView,
  type FileStepKind,
  fileStepView,
} from "./memoryStatusView";

const CAPABILITIES_LINK_TEXT = "Open Capabilities to install zram";
// The long changes are found in the mutation cache by these keys. The four
// swap file jobs share a prefix, so one count holds the controls for all.
const MODE_MUTATION_KEY = [TASK_TYPE_MEMORY_SWAP_MODE] as const;
const DRAIN_MUTATION_KEY = [TASK_TYPE_MEMORY_DRAIN] as const;
const SWAP_FILE_MUTATION_KEY = ["memory.swap_file"] as const;

const MIB = 1024 ** 2;

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

/**
 * The step the dialog shows in place of its first one: a change that asks
 * first or needs a size. A change of mode is kept as it was shown when it
 * opened, since its request carries the files the user agreed to remove. A
 * swap file step sends only a path and a size, so it is built from the
 * status on every render and follows the host while it is open.
 */
type Step =
  | { kind: "mode"; mode: RowMode; view: ConfirmView }
  | { kind: FileStepKind; path: string };

/** The control that opened a step, which takes focus when it is left. */
const stepOpener = (step: Step) =>
  step.kind === "mode"
    ? "apply"
    : step.kind === "create"
      ? "create"
      : `${step.kind}:${step.path}`;

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
          {row.note ? (
            <AppTypography color="text.secondary" variant="caption">
              {row.note}
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
 * Reports a swap file job the dialog follows: it marks the job reported, so
 * the task tray stays quiet, shows its steps and toasts its end with the file
 * it acted on.
 */
const swapFileJobConfig = (
  action: SwapFileAction,
  type: string,
  setProgressMessage: (message: string | null) => void,
) => {
  const pathOf = (request: object) =>
    ("path" in request && typeof request.path === "string" && request.path) ||
    undefined;
  return {
    onStart: (task: TaskSnapshot) => void markTerminalFeedbackEmitted(task.id),
    onProgress: (progress: Parameters<typeof swapProgressText>[0]) =>
      setProgressMessage(swapProgressText(progress) ?? null),
    success: (_status: SwapStatus, request: object) => {
      toast.success(
        swapFileDone(action, pathOf(request)).succeeded,
        toastLinkOptions(MEMORY_TOAST_META),
      );
    },
    // A cancelled job is not a failure: the task tray that cancelled it says
    // so.
    error: (error: LinuxIOError, request: object) => {
      if (isTaskCancellationError(error)) return;
      toast.error(
        actionErrorMessage(error, swapFileDone(action, pathOf(request)).failed),
        toastLinkOptions(MEMORY_TOAST_META),
      );
    },
    options: {
      mutationKey: [...SWAP_FILE_MUTATION_KEY, type],
      onSettled: () => setProgressMessage(null),
    },
  };
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
  const sizeFieldId = useId();
  const [selected, setSelected] = useState<RowMode | null>(null);
  const [step, setStep] = useState<Step | null>(null);
  const [sizeText, setSizeText] = useState("");
  // The control that takes focus when the first step shows again: the one
  // that opened a step that was left, or Close once a job started.
  const [returnFocus, setReturnFocus] = useState<string | null>(null);
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

  // A task, not a call: removing a swap file moves its data back first.
  // This dialog reports a job it follows, started here or taken up, so the
  // task tray does not report it as well.
  const setMode = linuxio.memory.set_swap_mode.useMutation({
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    onProgress: (progress) =>
      setProgressMessage(swapProgressText(progress) ?? null),
    success: MEMORY_MODE_APPLIED,
    warning: (result, request) => modeResultWarning(result, request.mode),
    error: MEMORY_MODE_FAILED,
    toast: MEMORY_TOAST_META,
    options: {
      mutationKey: MODE_MUTATION_KEY,
      onSettled: () => {
        setProgressMessage(null);
        // The choice was kept for a dialog closed while the change ran.
        if (!open) setSelected(null);
      },
    },
  });
  const drain = linuxio.memory.drain_swap.useMutation({
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    onProgress: (progress) =>
      setProgressMessage(swapProgressText(progress) ?? null),
    success: MEMORY_DRAINED,
    error: MEMORY_DRAIN_FAILED,
    toast: MEMORY_TOAST_META,
    options: {
      mutationKey: DRAIN_MUTATION_KEY,
      onSettled: () => setProgressMessage(null),
    },
  });
  const createFile = linuxio.memory.create_swap_file.useMutation(
    swapFileJobConfig(
      "create",
      TASK_TYPE_MEMORY_CREATE_SWAP_FILE,
      setProgressMessage,
    ),
  );
  const resizeFile = linuxio.memory.resize_swap_file.useMutation(
    swapFileJobConfig(
      "resize",
      TASK_TYPE_MEMORY_RESIZE_SWAP_FILE,
      setProgressMessage,
    ),
  );
  const removeFile = linuxio.memory.remove_swap_file.useMutation(
    swapFileJobConfig(
      "remove",
      TASK_TYPE_MEMORY_REMOVE_SWAP_FILE,
      setProgressMessage,
    ),
  );
  const moveFile = linuxio.memory.move_swap_file.useMutation(
    swapFileJobConfig(
      "move",
      TASK_TYPE_MEMORY_MOVE_SWAP_FILE,
      setProgressMessage,
    ),
  );
  const setSwappiness = linuxio.memory.set_swappiness.useMutation({
    success: "Eagerness updated",
    error: "Failed to change the eagerness",
    toast: MEMORY_TOAST_META,
    options: {
      // A step made while the save ran is not lost: only the saved value's
      // draft is dropped, so the slider then follows the host.
      onSettled: (_status, _error, request) =>
        setDraftSwappiness((draft) =>
          draft === request.swappiness ? null : draft,
        ),
    },
  });
  // A change that is running is counted in the mutation cache and not only
  // in this dialog: one started before the dashboard was left is still
  // followed there, by the dialog that started it, and holds the controls of
  // this one too. The slider stays enabled while its own save runs: disabling
  // the range input the user is holding would drop keyboard focus.
  const modeRunning = useIsMutating({ mutationKey: MODE_MUTATION_KEY }) > 0;
  const drainRunning = useIsMutating({ mutationKey: DRAIN_MUTATION_KEY }) > 0;
  const fileRunning =
    useIsMutating({ mutationKey: SWAP_FILE_MUTATION_KEY }) > 0;
  const otherChangeRunning = modeRunning || drainRunning || fileRunning;
  const busy = otherChangeRunning || setSwappiness.isPending;

  // A change is a job and runs on when this dialog, the page or the tab is
  // closed. One still running when the dialog opens, which nothing in this
  // tab follows, is taken up again, so the dialog shows its step, holds its
  // controls and reports its result.
  const scanKey = open ? "open" : null;
  useActiveTaskRecovery({
    type: TASK_TYPE_MEMORY_SWAP_MODE,
    scanKey,
    match: () => true,
    onRecover: (task) => {
      const mode = task.metadata?.action as SwapMode | undefined;
      if (otherChangeRunning || !mode || !isRowMode(mode)) return;
      setSelected(mode);
      setMode.watch(task.id, { mode });
    },
  });
  useActiveTaskRecovery({
    type: TASK_TYPE_MEMORY_DRAIN,
    scanKey,
    match: () => true,
    onRecover: (task) => {
      if (otherChangeRunning) return;
      drain.watch(task.id, undefined);
    },
  });
  // A swap file job's metadata names its file, which its result toast needs.
  const recoveredPath = (task: TaskSnapshot) =>
    requestString(taskMetadataObject(task.metadata), "path") ?? "";
  useActiveTaskRecovery({
    type: TASK_TYPE_MEMORY_CREATE_SWAP_FILE,
    scanKey,
    match: () => true,
    onRecover: (task) => {
      if (otherChangeRunning) return;
      createFile.watch(task.id, { size: 0 });
    },
  });
  useActiveTaskRecovery({
    type: TASK_TYPE_MEMORY_RESIZE_SWAP_FILE,
    scanKey,
    match: () => true,
    onRecover: (task) => {
      if (otherChangeRunning) return;
      resizeFile.watch(task.id, { path: recoveredPath(task), size: 0 });
    },
  });
  useActiveTaskRecovery({
    type: TASK_TYPE_MEMORY_REMOVE_SWAP_FILE,
    scanKey,
    match: () => true,
    onRecover: (task) => {
      if (otherChangeRunning) return;
      removeFile.watch(task.id, { path: recoveredPath(task) });
    },
  });
  useActiveTaskRecovery({
    type: TASK_TYPE_MEMORY_MOVE_SWAP_FILE,
    scanKey,
    match: () => true,
    onRecover: (task) => {
      if (otherChangeRunning) return;
      moveFile.watch(task.id, { path: recoveredPath(task) });
    },
  });

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

  // A running change does not hold the dialog open. It is a job: the task
  // tray shows its progress and can cancel it, and its result is toasted by
  // whatever follows it, this dialog or the tray. The choice a mode change
  // is applying is kept, so the dialog shows that change when it is opened
  // again.
  const handleClose = () => {
    // A key press still waiting is saved, not dropped.
    if (keyWait !== null) commitSwappiness();
    if (!modeRunning) setSelected(null);
    setStep(null);
    setReturnFocus(null);
    setAdvancedOpen(false);
    // A save that is running sends the newest step itself when it ends.
    if (!setSwappiness.isPending) {
      setDraftSwappiness(null);
      newestStep.current = null;
    }
    onClose();
  };

  const dialogProps = {
    "aria-busy": busy || undefined,
    fullWidth: true,
    maxWidth: "sm",
    onClose: handleClose,
    open,
  } as const;
  // Keyed so the button, which has focus while the status loads, is the same
  // element once the rows and Apply appear beside it.
  const closeButton = (
    <AppButton
      autoFocus={returnFocus === "close"}
      key="close"
      onClick={handleClose}
    >
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
  const generatorNote = generatorFilesNote(status);

  const stepView = (current: Step) =>
    current.kind === "mode"
      ? current.view
      : fileStepView(status, current.kind, current.path);
  const openStep = (next: Step) => {
    const size = stepView(next).size;
    setSizeText(size ? String(size.start) : "");
    setStep(next);
  };

  // A change of mode that removes swap files, creates one or replaces a zram
  // configuration file that is not LinuxIO's asks first, as a second step of
  // this dialog that lists all of it; every other change applies at once.
  const requestMode = (mode: RowMode) => {
    setSelected(mode);
    const view = confirmView(status, mode);
    if (view) {
      openStep({ kind: "mode", mode, view });
    } else {
      setMode.mutate({ mode });
    }
  };

  if (step) {
    const view = stepView(step);
    const changed = "changed" in view && view.changed === true;
    const size = view.size ? checkSize(sizeText, view.size) : undefined;
    const unchanged =
      view.sameSize !== undefined && size?.bytes === view.sameSize * MIB;
    const ready =
      !busy &&
      !changed &&
      (size === undefined || (size.bytes !== undefined && !unchanged));
    const leave = () => {
      setReturnFocus(stepOpener(step));
      setStep(null);
    };
    // The step closes as its job starts: the task tray holds the progress
    // and the cancel, and the first step holds its controls meanwhile.
    const start = () => {
      if (!ready) return;
      const bytes = size?.bytes;
      switch (step.kind) {
        case "mode": {
          const request: SwapModeRequest = { mode: step.mode };
          if (step.view.replace) request.confirm_replace = step.view.replace;
          if (step.view.remove) request.confirm_remove = step.view.remove;
          if (bytes !== undefined) request.swap_file_size = bytes;
          setMode.mutate(request);
          break;
        }
        case "create":
          createFile.mutate({ size: bytes ?? 0 });
          break;
        case "resize":
          resizeFile.mutate({ path: step.path, size: bytes ?? 0 });
          break;
        case "remove":
          removeFile.mutate({ path: step.path });
          break;
        case "move":
          moveFile.mutate({ path: step.path });
          break;
      }
      setReturnFocus("close");
      setStep(null);
    };
    return (
      <GeneralDialog {...dialogProps}>
        <AppDialogTitle>{view.title}</AppDialogTitle>
        <AppDialogContent>
          <ul className="memory-overflow__steps">
            {view.items.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
          {view.size && size ? (
            <AppTextField
              autoFocus
              error={size.error !== undefined}
              fullWidth
              helperText={size.error ?? size.help}
              id={sizeFieldId}
              label="Size in MiB"
              onChange={(event) => setSizeText(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") start();
              }}
              type="number"
              value={sizeText}
            />
          ) : null}
        </AppDialogContent>
        <AppDialogActions>
          {/* Without a size to set, focus lands on the way back. */}
          <AppButton autoFocus={!view.size} onClick={leave}>
            Back
          </AppButton>
          <AppButton disabled={!ready} onClick={start} variant="contained">
            {view.action}
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
                  drainPending={drainRunning}
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

          {generatorNote ? (
            <AppAlert severity="info">
              {generatorNote.text}
              <ul className="memory-overflow__files">
                {generatorNote.files.map((file) => (
                  <li key={file}>{file}</li>
                ))}
              </ul>
            </AppAlert>
          ) : null}

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

          <SwapFilesSection
            busy={busy}
            onCreate={() =>
              openStep({ kind: "create", path: status.create.path })
            }
            onFileAction={(kind, path) => openStep({ kind, path })}
            returnFocus={returnFocus}
            status={status}
          />

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
          {otherChangeRunning && progressMessage ? progressMessage : ""}
        </AppTypography>
        {closeButton}
        <AppButton
          autoFocus={returnFocus === "apply"}
          disabled={busy || !canApply(status, choice)}
          onClick={() => requestMode(choice)}
          variant="contained"
        >
          {modeRunning ? "Applying…" : "Apply"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

/**
 * The swap files, each with what LinuxIO may do to it or why it does
 * nothing, then the other swap devices, then Create or why it is not offered.
 */
const SwapFilesSection = ({
  busy,
  onCreate,
  onFileAction,
  returnFocus,
  status,
}: {
  busy: boolean;
  onCreate: () => void;
  onFileAction: (kind: "resize" | "remove" | "move", path: string) => void;
  returnFocus: string | null;
  status: SwapStatus;
}) => {
  const labelId = useId();
  const files = swapFileViews(status);
  const others = otherSwapViews(status);
  const create = createView(status);
  const action = (
    kind: "resize" | "remove" | "move",
    path: string,
    label: string,
  ) => (
    <AppButton
      aria-label={`${label} ${path}`}
      autoFocus={returnFocus === `${kind}:${path}`}
      disabled={busy}
      onClick={() => onFileAction(kind, path)}
      size="small"
    >
      {label}
    </AppButton>
  );
  return (
    <div className="memory-overflow__group">
      <span className="memory-overflow__name" id={labelId}>
        Swap files
      </span>
      <ul aria-labelledby={labelId} className="memory-swap">
        {files.map((file) => (
          <li className="memory-swap__row" key={file.path}>
            <div className="memory-swap__head">
              <span className="memory-swap__path">{file.path}</span>
              <AppTypography color="text.secondary" variant="body2">
                {file.summary}
              </AppTypography>
            </div>
            {file.notes.map((note) => (
              <AppTypography
                color="text.secondary"
                key={note}
                variant="caption"
              >
                {note}
              </AppTypography>
            ))}
            {file.snapshots ? (
              <AppTypography color="warning" variant="caption">
                {file.snapshots}
              </AppTypography>
            ) : null}
            {file.resize || file.remove || file.move ? (
              <div className="memory-swap__actions">
                {file.resize ? action("resize", file.path, "Resize") : null}
                {file.remove ? action("remove", file.path, "Remove") : null}
                {file.move ? action("move", file.path, "Move") : null}
              </div>
            ) : null}
          </li>
        ))}
        {others.map((device) => (
          <li className="memory-swap__row" key={device.path}>
            <div className="memory-swap__head">
              <span className="memory-swap__path">{device.path}</span>
              <AppTypography color="text.secondary" variant="body2">
                {`${device.kind}, ${device.summary}`}
              </AppTypography>
            </div>
          </li>
        ))}
        {files.length === 0 && others.length === 0 ? (
          <li className="memory-swap__row">
            <AppTypography color="text.secondary" variant="body2">
              No swap is on, and no swap file is in /etc/fstab.
            </AppTypography>
          </li>
        ) : null}
      </ul>
      {create.kind === "offer" ? (
        <AppButton
          autoFocus={returnFocus === "create"}
          className="memory-swap__create"
          disabled={busy}
          onClick={onCreate}
          size="small"
          variant="outlined"
        >
          Create a swap file
        </AppButton>
      ) : create.kind === "blocked" ? (
        <AppTypography color="text.secondary" variant="caption">
          {create.reason}
        </AppTypography>
      ) : null}
    </div>
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
