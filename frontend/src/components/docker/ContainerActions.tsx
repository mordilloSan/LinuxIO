import { Icon } from "@iconify/react";
import { useNavigate } from "@tanstack/react-router";
import { lazy, Suspense, useState } from "react";

import { linuxio, type ContainerInfo, useCallMutation } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTypography from "@/components/ui/AppTypography";

import { useDockerUpdateOperation } from "./DockerUpdateOperationProvider";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

interface ContainerActionsProps {
  actionPending?: boolean;
  container: Pick<
    ContainerInfo,
    "Id" | "Labels" | "State" | "updateAvailable" | "url"
  >;
  mode?: "buttons" | "icons" | "menu";
  name: string;
  onOpenLogs: () => void;
  onOpenTerminal: () => void;
}

const ContainerFormDialog = lazy(() => import("./ContainerFormDialog"));

interface SecondaryAction {
  danger?: boolean;
  disabled?: boolean;
  icon: string;
  label: string;
  onClick: () => void;
}

const ContainerActions = ({
  actionPending = false,
  container,
  mode = "icons",
  name,
  onOpenLogs,
  onOpenTerminal,
}: ContainerActionsProps) => {
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const [confirmation, setConfirmation] = useState<"kill" | "remove" | null>(
    null,
  );
  const [editOpen, setEditOpen] = useState(false);
  const [forceRemove, setForceRemove] = useState(false);
  const navigate = useNavigate();
  const { isUpdating, startUpdate, updating } = useDockerUpdateOperation();

  const { mutate: startContainer, isPending: isStartPending } = useCallMutation(
    linuxio.docker.start_container,
    {
      success: `Container ${name} started`,
      error: `Failed to start ${name}`,
      toast: DOCKER_TOAST_META,
    },
  );
  const { mutate: stopContainer, isPending: isStopPending } = useCallMutation(
    linuxio.docker.stop_container,
    {
      success: `Container ${name} stopped`,
      error: `Failed to stop ${name}`,
      toast: DOCKER_TOAST_META,
    },
  );
  const { mutate: restartContainer, isPending: isRestartPending } =
    useCallMutation(linuxio.docker.restart_container, {
      success: `Container ${name} restarted`,
      error: `Failed to restart ${name}`,
      toast: DOCKER_TOAST_META,
    });
  const { mutate: pauseContainer, isPending: isPausePending } = useCallMutation(
    linuxio.docker.pause_container,
    {
      success: `Container ${name} paused`,
      error: `Failed to pause ${name}`,
      toast: DOCKER_TOAST_META,
    },
  );
  const { mutate: unpauseContainer, isPending: isUnpausePending } =
    useCallMutation(linuxio.docker.unpause_container, {
      success: `Container ${name} resumed`,
      error: `Failed to resume ${name}`,
      toast: DOCKER_TOAST_META,
    });
  const { mutate: killContainer, isPending: isKillPending } = useCallMutation(
    linuxio.docker.kill_container,
    {
      success: `Container ${name} killed`,
      error: `Failed to kill ${name}`,
      toast: DOCKER_TOAST_META,
      options: { onSuccess: () => setConfirmation(null) },
    },
  );
  const { mutate: removeContainer, isPending: isRemovePending } =
    useCallMutation(linuxio.docker.remove_container, {
      success: `Container ${name} removed`,
      error: `Failed to remove ${name}`,
      toast: DOCKER_TOAST_META,
      options: { onSuccess: () => setConfirmation(null) },
    });

  const canStart =
    container.State === "created" || container.State === "exited";
  const canStop = container.State === "running" || container.State === "paused";
  const canRestart = canStop;
  const composeProject =
    container.Labels?.["com.docker.compose.project"]?.trim();
  const isUpdatePending = isUpdating(container.Id);
  const busy =
    actionPending ||
    isStartPending ||
    isStopPending ||
    isRestartPending ||
    isPausePending ||
    isUnpausePending ||
    isKillPending ||
    isRemovePending ||
    updating;
  const pendingActionLabel =
    actionPending || isStopPending
      ? "Stopping"
      : isStartPending
        ? "Starting"
        : isRestartPending
          ? "Restarting"
          : isPausePending
            ? "Pausing"
            : isUnpausePending
              ? "Resuming"
              : isKillPending
                ? "Killing"
                : isRemovePending
                  ? "Removing"
                  : updating
                    ? "Updating"
                    : undefined;
  const primary = canStop
    ? {
        color: "error" as const,
        disabled: false,
        icon: "mdi:stop-circle",
        label: "Stop",
        loading: actionPending || isStopPending,
        onClick: () => stopContainer({ containerId: container.Id }),
      }
    : {
        color: "success" as const,
        disabled: !canStart,
        icon: "mdi:play",
        label: "Start",
        loading: isStartPending,
        onClick: () => startContainer({ containerId: container.Id }),
      };

  const secondaryActions: SecondaryAction[] = [
    {
      icon: "mdi:pencil",
      label: composeProject ? "Edit stack" : "Edit",
      onClick: composeProject
        ? () => {
            void navigate({
              to: "/docker/compose",
              search: { stack: composeProject },
            });
          }
        : () => setEditOpen(true),
    },
    {
      disabled: !canRestart,
      icon: "mdi:restart",
      label: "Restart",
      onClick: () => restartContainer({ containerId: container.Id }),
    },
    ...(container.State === "running"
      ? [
          {
            icon: "mdi:pause",
            label: "Pause",
            onClick: () => pauseContainer({ containerId: container.Id }),
          },
        ]
      : []),
    ...(container.State === "paused"
      ? [
          {
            icon: "mdi:play-pause",
            label: "Unpause",
            onClick: () => unpauseContainer({ containerId: container.Id }),
          },
        ]
      : []),
    ...(container.updateAvailable && container.State === "running"
      ? [
          {
            disabled: isUpdatePending,
            icon: "mdi:update",
            label: isUpdatePending ? "Updating" : "Update",
            onClick: () => startUpdate(container.Id, name),
          },
        ]
      : []),
    {
      icon: "mdi:file-document-outline",
      label: "Logs",
      onClick: onOpenLogs,
    },
    {
      disabled: container.State !== "running",
      icon: "mdi:console",
      label: "Terminal",
      onClick: onOpenTerminal,
    },
    ...(container.url
      ? [
          {
            icon: "mdi:open-in-new",
            label: "Open App",
            onClick: () => window.open(container.url, "_blank", "noopener"),
          },
        ]
      : []),
    ...(container.State === "running"
      ? [
          {
            danger: true,
            icon: "mdi:skull-outline",
            label: "Kill",
            onClick: () => setConfirmation("kill"),
          },
        ]
      : []),
    {
      danger: true,
      disabled: container.State === "removing",
      icon: "mdi:delete",
      label: "Remove",
      onClick: () => {
        setForceRemove(false);
        setConfirmation("remove");
      },
    },
  ];

  const chooseAction = (action: SecondaryAction) => {
    setMenuAnchor(null);
    action.onClick();
  };
  const confirmAction = () => {
    if (confirmation === "kill") {
      killContainer({ containerId: container.Id });
    } else if (confirmation === "remove") {
      removeContainer({ containerId: container.Id, force: forceRemove });
    }
  };
  const needsForce = canStop;
  const confirmationPending = isKillPending || isRemovePending;

  return (
    <>
      <div
        style={{
          alignItems: "center",
          display: "flex",
          flexWrap: mode === "buttons" ? "wrap" : "nowrap",
          gap: mode === "buttons" ? 6 : 2,
          marginTop: mode === "buttons" ? 12 : undefined,
        }}
      >
        {mode !== "menu" &&
          (mode === "buttons" ? (
            <AppButton
              aria-label={`${primary.label} ${name}`}
              color={primary.color}
              disabled={busy || primary.disabled}
              onClick={primary.onClick}
              size="small"
              startIcon={
                primary.loading ? (
                  <AppCircularProgress color="inherit" size={14} />
                ) : (
                  <Icon height={16} icon={primary.icon} width={16} />
                )
              }
              variant="outlined"
            >
              {primary.label}
            </AppButton>
          ) : (
            <AppActionIconButton
              disabled={busy || primary.disabled}
              icon={primary.icon}
              iconSize={16}
              label={primary.label}
              loading={primary.loading}
              onClick={primary.onClick}
            />
          ))}
        {mode === "buttons" ? (
          <AppButton
            aria-label={
              pendingActionLabel
                ? `${pendingActionLabel} ${name}`
                : `Actions for ${name}`
            }
            aria-expanded={Boolean(menuAnchor)}
            aria-haspopup="menu"
            disabled={busy}
            onClick={(event) => setMenuAnchor(event.currentTarget)}
            size="small"
            startIcon={
              busy ? (
                <AppCircularProgress color="inherit" size={14} />
              ) : (
                <Icon height={16} icon="mdi:dots-horizontal" width={16} />
              )
            }
            variant="outlined"
          >
            Actions
          </AppButton>
        ) : (
          <AppActionIconButton
            ariaLabel={
              pendingActionLabel
                ? `${pendingActionLabel} ${name}`
                : `Actions for ${name}`
            }
            disabled={busy}
            icon="mdi:dots-vertical"
            iconSize={20}
            loading={busy}
            onClick={(event) => setMenuAnchor(event.currentTarget)}
            tooltip={false}
          />
        )}
      </div>

      <AppMenu
        anchorEl={menuAnchor}
        ariaLabel={`Actions for ${name}`}
        minWidth={176}
        onClose={() => setMenuAnchor(null)}
        open={Boolean(menuAnchor)}
      >
        {mode === "menu" && (
          <AppMenuItem
            disabled={busy || primary.disabled}
            onClick={() => {
              setMenuAnchor(null);
              primary.onClick();
            }}
            startAdornment={<Icon icon={primary.icon} width={18} />}
          >
            {primary.label}
          </AppMenuItem>
        )}
        {secondaryActions.map((action) => (
          <AppMenuItem
            danger={action.danger}
            disabled={busy || action.disabled}
            key={action.label}
            onClick={() => chooseAction(action)}
            startAdornment={<Icon icon={action.icon} width={18} />}
          >
            {action.label}
          </AppMenuItem>
        ))}
      </AppMenu>

      <GeneralDialog
        aria-label={
          confirmation === "kill" ? `Kill ${name}?` : `Remove ${name}?`
        }
        aria-busy={confirmationPending || undefined}
        disableEscapeKeyDown={confirmationPending}
        fullWidth
        maxWidth="xs"
        onClose={confirmationPending ? undefined : () => setConfirmation(null)}
        open={confirmation !== null}
      >
        <AppDialogTitle>
          {confirmation === "kill" ? `Kill ${name}?` : `Remove ${name}?`}
        </AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            {confirmation === "kill"
              ? "SIGKILL stops the container immediately without allowing its processes to clean up."
              : "This permanently deletes the container and its writable layer. Named volumes and bind-mounted data are kept."}
          </AppDialogContentText>
          {confirmation === "remove" && needsForce && (
            <label
              style={{
                alignItems: "center",
                display: "flex",
                gap: "var(--app-space-8)",
                marginTop: "var(--app-space-12)",
              }}
            >
              <AppCheckbox
                checked={forceRemove}
                color="error"
                onChange={(_, checked) => setForceRemove(checked)}
              />
              <AppTypography variant="body2">
                Force removal of this active container
              </AppTypography>
            </label>
          )}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton
            disabled={confirmationPending}
            onClick={() => setConfirmation(null)}
          >
            Cancel
          </AppButton>
          <AppButton
            autoFocus
            color="error"
            disabled={
              confirmationPending ||
              (needsForce && !forceRemove && confirmation === "remove")
            }
            onClick={confirmAction}
            variant="contained"
          >
            {confirmationPending
              ? "Working…"
              : confirmation === "kill"
                ? "Kill container"
                : "Remove container"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>

      {editOpen && (
        <Suspense fallback={null}>
          <ContainerFormDialog
            containerId={container.Id}
            mode="edit"
            onClose={() => setEditOpen(false)}
            open
          />
        </Suspense>
      )}
    </>
  );
};

export default ContainerActions;
