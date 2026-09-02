import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import "./capability-manager-section.css";

import {
  CAPABILITIES,
  type CapabilitiesResponse,
  capabilitiesQueryKey,
  type CapabilityErrorKey,
  type CapabilityValueKey,
  type InstallCapabilityOutput,
  type TaskSnapshot,
  linuxio,
  useStreamMux,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import CapabilityInstallDialog, {
  type CapabilityInstallOutputLine,
  type CapabilityInstallOutputStream,
} from "@/components/dialog/CapabilityInstallDialog";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { TASK_TYPE_SYSTEM_INSTALL_CAPABILITY } from "@/constants/backgroundTaskTypes";
import { useActiveTaskRecovery } from "@/hooks/backgroundTasks/useActiveTaskRecovery";
import useAuth from "@/hooks/useAuth";
import {
  type CapabilityStatus,
  getCapabilityReason,
  getCapabilityStatus,
  useCapabilitiesResponse,
} from "@/hooks/useCapabilities";
import { useConfigUserId } from "@/hooks/useConfig";
import { withPromiseCleanup } from "@/utils/withPromiseCleanup";

const MAX_INSTALL_OUTPUT_LINES = 500;
const TERMINAL_CONTROL_SEQUENCE = new RegExp(
  String.raw`(?:\u001B\][^\u0007]*(?:\u0007|\u001B\\)|(?:\u001B\[|\u009B)[0-?]*[ -/]*[@-~]|\u001B[@-_])`,
  "g",
);

interface CapabilityInstallRun {
  error: string | null;
  id: number;
  label: string;
  message: string;
  nextOutputID: number;
  output: CapabilityInstallOutputLine[];
  outputHistoryIncomplete: boolean;
  percentage: number | null;
  running: boolean;
  stage: string;
  success: boolean;
  task?: TaskSnapshot;
  warning: string | null;
  wire: string;
}

const installOutputStream = (
  output: InstallCapabilityOutput,
): CapabilityInstallOutputStream =>
  output.stream === "stdout" || output.stream === "stderr"
    ? output.stream
    : "status";

const stripTerminalControlSequences = (text: string): string =>
  text.replace(TERMINAL_CONTROL_SEQUENCE, "");

const appendInstallOutput = (
  run: CapabilityInstallRun,
  output: InstallCapabilityOutput | undefined,
): CapabilityInstallRun => {
  if (!output) return run;
  const text = stripTerminalControlSequences(output.text);
  if (text.length === 0) return run;
  const records = [
    ...run.output,
    {
      id: run.nextOutputID,
      stream: installOutputStream(output),
      text,
    },
  ];
  const truncated = records.length > MAX_INSTALL_OUTPUT_LINES;
  return {
    ...run,
    output: truncated
      ? records.slice(records.length - MAX_INSTALL_OUTPUT_LINES)
      : records,
    outputHistoryIncomplete: run.outputHistoryIncomplete || truncated,
    nextOutputID: run.nextOutputID + 1,
  };
};

const STATUS_DETAILS: Record<
  CapabilityStatus,
  { label: string; color: "default" | "success" | "warning" }
> = {
  available: { label: "Available", color: "success" },
  unavailable: { label: "Unavailable", color: "warning" },
  unknown: { label: "Unknown", color: "default" },
};

const formatLastChecked = (value: Date | null) => {
  if (!value) return "Saved sign-in snapshot";
  return value.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
};

const CapabilityManagerSection = () => {
  const { refreshCapabilities } = useAuth();
  // The latest scan lives in the query cache; refreshCapabilities writes it
  // and an install result patches it below.
  const latest = useCapabilitiesResponse();
  const queryClient = useQueryClient();
  const capabilitiesKey = capabilitiesQueryKey(useConfigUserId());

  const [lastChecked, setLastChecked] = useState<Date | null>(null);
  const [isRefreshing, setIsRefreshing] = useState(true);
  const [errorText, setErrorText] = useState<string | null>(null);
  const [installDialogOpen, setInstallDialogOpen] = useState(false);
  const [installRun, setInstallRun] = useState<CapabilityInstallRun | null>(
    null,
  );
  const mountedRef = useRef(true);
  const nextInstallRunID = useRef(0);
  const launchedInstallRunID = useRef<number | null>(null);
  const dismissedInstallRef = useRef<{
    taskID?: string;
    wire: string;
  } | null>(null);
  const { isOpen: muxIsOpen } = useStreamMux();

  // The dialog owns live presentation only. Completion feedback and the
  // app-wide capability refresh remain in useRecoveredTasks, so closing this
  // panel or dialog never takes ownership away from the system Task.
  const installCapability =
    linuxio.system.install_capability.useTaskStreamAction({
      error: (streamError, variables) => {
        if (!mountedRef.current) return;
        const dismissed = dismissedInstallRef.current;
        if (
          dismissed?.wire === variables.capability &&
          dismissed.taskID === undefined
        ) {
          dismissedInstallRef.current = null;
        }
        setInstallRun((previous) => {
          if (!previous || previous.wire !== variables.capability) {
            return previous;
          }
          return {
            ...previous,
            error: streamError.message || "Capability installation failed",
            running: false,
            success: false,
            warning: null,
          };
        });
      },
      onProgress: (progress, task, variables) => {
        if (!mountedRef.current) return;
        setInstallRun((previous) => {
          if (
            !previous ||
            !previous.running ||
            previous.wire !== variables.capability
          ) {
            return previous;
          }
          const detail = progress.detail;
          const next = {
            ...previous,
            message: progress.message ?? detail?.message ?? previous.message,
            task: task ?? previous.task,
            percentage:
              typeof progress.percentage === "number"
                ? progress.percentage
                : typeof detail?.percentage === "number"
                  ? detail.percentage
                  : previous.percentage,
            stage: detail?.stage ?? progress.phase ?? previous.stage,
          };
          return appendInstallOutput(next, detail?.output);
        });
      },
      onTaskStart: (task, variables) => {
        if (!mountedRef.current) return;
        const dismissed = dismissedInstallRef.current;
        if (
          dismissed?.wire === variables.capability &&
          dismissed.taskID === undefined
        ) {
          dismissedInstallRef.current = { ...dismissed, taskID: task.id };
        }
        setInstallRun((previous) =>
          previous?.wire === variables.capability
            ? { ...previous, task }
            : previous,
        );
      },
      success: (result, variables) => {
        if (!mountedRef.current) return;
        if (dismissedInstallRef.current?.wire === variables.capability) {
          dismissedInstallRef.current = null;
        }
        queryClient.setQueryData<Partial<CapabilitiesResponse>>(
          capabilitiesKey,
          (previous) => ({
            ...previous,
            [`${variables.capability}_available`]: result.available,
            [`${variables.capability}_error`]: result.error ?? "",
          }),
        );
        setLastChecked(new Date());
        setInstallRun((previous) => {
          if (!previous || previous.wire !== variables.capability) {
            return previous;
          }
          const resultWarning = result.warning;
          let warning: string | null = resultWarning ?? null;
          if (!result.available) {
            warning =
              resultWarning ||
              result.error ||
              `${previous.label} was installed but is still unavailable`;
          }
          return {
            ...previous,
            error: null,
            message: warning ?? "Installation completed",
            percentage: 100,
            running: false,
            success: result.available && !warning,
            warning,
          };
        });
      },
    });

  const installingWire = installRun?.running ? installRun.wire : null;

  useActiveTaskRecovery({
    match: (task) => {
      const wire = task.metadata?.capability;
      return Boolean(
        wire &&
        CAPABILITIES.some(
          (capability) =>
            capability.wire === wire && "installable" in capability,
        ),
      );
    },
    onRecover: (task) => {
      const wire = task.metadata?.capability;
      const capability = CAPABILITIES.find((item) => item.wire === wire);
      if (!wire || !capability) return;
      const dismissed = dismissedInstallRef.current;
      const wasDismissed =
        dismissed?.wire === wire &&
        (!dismissed.taskID || dismissed.taskID === task.id);
      const id = ++nextInstallRunID.current;
      setInstallRun((previous) =>
        previous?.running
          ? previous
          : {
              error: null,
              id,
              label: capability.label,
              message: "Reconnecting to installation…",
              nextOutputID: 0,
              output: [],
              outputHistoryIncomplete: true,
              percentage: null,
              running: true,
              stage: "reconnecting",
              success: false,
              task,
              warning: null,
              wire,
            },
      );
      if (!wasDismissed) {
        setInstallDialogOpen(true);
      }
    },
    scanKey: muxIsOpen ? "capability-installation" : null,
    type: TASK_TYPE_SYSTEM_INSTALL_CAPABILITY,
  });

  useEffect(() => {
    if (
      !installRun?.running ||
      launchedInstallRunID.current === installRun.id
    ) {
      return;
    }
    launchedInstallRunID.current = installRun.id;
    const request = { capability: installRun.wire };
    if (installRun.task) {
      installCapability.watch(installRun.task, request);
    } else {
      installCapability.mutate(request);
    }
  }, [installCapability, installRun]);

  const packageKitAvailable = latest.packagekit_available ?? false;
  const dockerAvailable = latest.docker_available ?? false;

  const rows = useMemo(
    () =>
      CAPABILITIES.filter(
        (item) => !("visibleInManager" in item) || item.visibleInManager,
      ).map((item) => {
        const valueKey = `${item.wire}_available` as CapabilityValueKey;
        const errorKey = `${item.wire}_error` as CapabilityErrorKey;
        const status = getCapabilityStatus(latest[valueKey]);
        const detail =
          latest[errorKey] ||
          (status === "available"
            ? item.readyText
            : getCapabilityReason(item.state, status));
        const installable =
          "installable" in item ? item.installable : undefined;

        return {
          ...item,
          installable,
          status,
          detail,
        };
      }),
    [latest],
  );

  const handleRefresh = useCallback(
    async (showSuccessToast = true) => {
      setIsRefreshing(true);
      setErrorText(null);

      return withPromiseCleanup(
        (async () => {
          try {
            await refreshCapabilities();
            if (!mountedRef.current) return;
            setLastChecked(new Date());
            if (showSuccessToast) {
              toast.success("Capabilities refreshed");
            }
          } catch (error: unknown) {
            const message =
              error instanceof Error
                ? error.message
                : "Failed to refresh capabilities";
            if (!mountedRef.current) return;
            setErrorText(message);
            if (showSuccessToast) {
              toast.error(message);
            }
          }
        })(),
        () => {
          if (mountedRef.current) {
            setIsRefreshing(false);
          }
        },
      );
    },
    [refreshCapabilities],
  );

  const handleInstall = useCallback((wire: string, label: string) => {
    dismissedInstallRef.current = null;
    const id = ++nextInstallRunID.current;
    setInstallRun({
      error: null,
      id,
      label,
      message: "Starting installation…",
      nextOutputID: 0,
      output: [],
      outputHistoryIncomplete: false,
      percentage: null,
      running: true,
      stage: "starting",
      success: false,
      warning: null,
      wire,
    });
    // The mutation starts from an Effect after this state is painted, so the
    // dialog is present before the first backend request is submitted.
    setInstallDialogOpen(true);
  }, []);

  const handleInstallDialogClose = useCallback(() => {
    if (installRun?.running) {
      dismissedInstallRef.current = {
        taskID: installRun.task?.id,
        wire: installRun.wire,
      };
    }
    setInstallDialogOpen(false);
  }, [installRun]);

  const handleViewInstall = useCallback(() => {
    dismissedInstallRef.current = null;
    setInstallDialogOpen(true);
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    refreshCapabilities()
      .then(() => {
        if (cancelled || !mountedRef.current) return;
        setLastChecked(new Date());
      })
      .finally(() => {
        if (!cancelled && mountedRef.current) setIsRefreshing(false);
      })
      .catch((error: unknown) => {
        if (cancelled || !mountedRef.current) return;
        setErrorText(
          error instanceof Error
            ? error.message
            : "Failed to refresh capabilities",
        );
      });
    return () => {
      cancelled = true;
    };
  }, [refreshCapabilities]);

  return (
    <div aria-busy={isRefreshing} className="capability-manager">
      <div className="capability-manager__header">
        <div>
          <AppTypography fontWeight={600} variant="body1">
            Capability Manager
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Last check: {formatLastChecked(lastChecked)}
          </AppTypography>
        </div>
        <AppTooltip title={isRefreshing ? "Checking" : "Refresh"}>
          <AppIconButton
            aria-label={
              isRefreshing ? "Checking capabilities" : "Refresh capabilities"
            }
            color="default"
            disabled={isRefreshing}
            onClick={() => void handleRefresh()}
            size="small"
          >
            <Icon
              className={isRefreshing ? "capability-manager__spin" : undefined}
              height={18}
              icon={isRefreshing ? "mdi:loading" : "mdi:refresh"}
              width={18}
            />
          </AppIconButton>
        </AppTooltip>
      </div>

      {errorText ? (
        <AppAlert severity="error">
          <AppAlertTitle>Capability check failed</AppAlertTitle>
          {errorText}
        </AppAlert>
      ) : null}

      <div className="capability-manager__list">
        {rows.map((row) => {
          const status = STATUS_DETAILS[row.status];
          const installing = installingWire === row.wire;
          const showInstall =
            installing ||
            (row.status === "unavailable" && row.installable !== undefined);
          const blockedByPackageKit =
            showInstall &&
            row.installable?.requiresPackageKit === true &&
            !packageKitAvailable;
          const blockedByDocker =
            showInstall &&
            row.installable !== undefined &&
            "requiresDocker" in row.installable &&
            row.installable.requiresDocker === true &&
            !dockerAvailable;
          const installDisabled =
            !installing &&
            (installingWire !== null || blockedByPackageKit || blockedByDocker);
          const installTooltip = installing
            ? "View installation progress"
            : blockedByPackageKit
              ? "Install requires PackageKit, which is itself unavailable. Install PackageKit from a shell first."
              : blockedByDocker
                ? "Install requires Docker to be available first."
                : `Install ${row.label}`;

          return (
            <FrostedCard
              className="capability-manager__row"
              hoverLift
              key={row.state}
            >
              <div className="capability-manager__icon">
                <Icon height={22} icon={row.icon} width={22} />
              </div>
              <div className="capability-manager__body">
                <div className="capability-manager__row-header">
                  <div className="capability-manager__title-block">
                    <AppTypography
                      component="h3"
                      fontWeight={600}
                      variant="body2"
                    >
                      {row.label}
                    </AppTypography>
                    <AppTypography color="text.secondary" variant="caption">
                      {row.description}
                    </AppTypography>
                  </div>
                  <div className="capability-manager__row-actions">
                    {showInstall ? (
                      <AppTooltip title={installTooltip}>
                        <span>
                          <AppButton
                            aria-label={
                              installing
                                ? `View ${row.label} installation`
                                : `Install ${row.label}`
                            }
                            color="primary"
                            disabled={installDisabled}
                            onClick={() => {
                              if (installing) {
                                handleViewInstall();
                              } else {
                                handleInstall(row.wire, row.label);
                              }
                            }}
                            size="small"
                            startIcon={
                              <Icon
                                className={
                                  installing
                                    ? "capability-manager__spin"
                                    : undefined
                                }
                                height={16}
                                icon={
                                  installing ? "mdi:loading" : "mdi:download"
                                }
                                width={16}
                              />
                            }
                            variant="outlined"
                          >
                            {installing
                              ? typeof installRun?.percentage === "number"
                                ? `${installRun?.percentage}%`
                                : "Installing…"
                              : "Install"}
                          </AppButton>
                        </span>
                      </AppTooltip>
                    ) : null}
                    <AppChip
                      color={status.color}
                      label={status.label}
                      size="small"
                      variant="soft"
                    />
                  </div>
                </div>
                <div className="capability-manager__detail">
                  <AppTypography
                    className="capability-manager__dependency"
                    component="span"
                    noWrap
                    style={{ color: "var(--app-palette-text-primary)" }}
                    title={row.dependency}
                    variant="body2"
                  >
                    {row.dependency}
                  </AppTypography>
                  <span>{installing ? installRun?.message : row.detail}</span>
                </div>
              </div>
            </FrostedCard>
          );
        })}
      </div>
      {installRun ? (
        <CapabilityInstallDialog
          capabilityLabel={installRun.label}
          error={installRun.error}
          message={installRun.message}
          onClose={handleInstallDialogClose}
          open={installDialogOpen}
          output={installRun.output}
          outputHistoryIncomplete={installRun.outputHistoryIncomplete}
          percentage={installRun.percentage}
          running={installRun.running}
          stage={installRun.stage}
          success={installRun.success}
          warning={installRun.warning}
        />
      ) : null}
    </div>
  );
};

export default CapabilityManagerSection;
