import { Icon } from "@iconify/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

import {
  linuxio,
  type DockerDaemonConfig,
  type DockerDaemonConfigSetResult,
  type DockerDaemonStorageProgress,
  type TaskProgress,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import PathPickerField from "@/components/pathpicker/PathPickerField";
import {
  SettingsGrid,
  SettingsSaveFooter,
  useSettingsDraft,
} from "@/components/settings/SettingsSectionForm";
import {
  SectionCard,
  SettingsNote,
  ToggleCard,
} from "@/components/settings/SettingsSectionPrimitives";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import ConfirmDialog from "@/components/ui/AppConfirmDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";

// The classic graph drivers and the containerd snapshotters name overlay and
// the plain-copy driver differently; dockerd refuses to start on a mismatch.
const CLASSIC_DRIVERS = ["overlay2", "btrfs", "zfs", "vfs"];
const CONTAINERD_SNAPSHOTTERS = ["overlayfs", "btrfs", "zfs", "native"];

const trimDataRoot = (value: string): string =>
  value.trim().replace(/(.)\/+$/, "$1");

const toRequest = (draft: DockerDaemonConfig): DockerDaemonConfig => ({
  ...draft,
  data_root: trimDataRoot(draft.data_root),
  log_max_size: draft.log_max_size.trim(),
  log_max_file: draft.log_max_file.trim(),
  address_pool_base: draft.address_pool_base.trim(),
});

interface DraftErrors {
  data_root?: string;
  log_max_size?: string;
  log_max_file?: string;
  address_pool_base?: string;
  address_pool_size?: string;
}

const validateDraft = (draft: DockerDaemonConfig): DraftErrors => {
  const errors: DraftErrors = {};
  const dataRoot = trimDataRoot(draft.data_root);
  if (dataRoot && (!dataRoot.startsWith("/") || /^\/+$/.test(dataRoot))) {
    errors.data_root = "Use an absolute path other than /.";
  }
  const maxSize = draft.log_max_size.trim();
  if (maxSize && !/^[1-9][0-9]*[kmgKMG]?$/.test(maxSize)) {
    errors.log_max_size = "Use a size like 10m or 500k.";
  }
  const maxFile = draft.log_max_file.trim();
  if (maxFile && !/^[1-9][0-9]*$/.test(maxFile)) {
    errors.log_max_file = "Use a whole number of files.";
  }
  const base = draft.address_pool_base.trim();
  if (base) {
    const prefix = Number(base.split("/")[1]);
    if (!/^\d{1,3}(\.\d{1,3}){3}\/\d{1,2}$/.test(base)) {
      errors.address_pool_base = "Use a CIDR like 172.20.0.0/16.";
    } else if (
      draft.address_pool_size < prefix ||
      draft.address_pool_size > 32
    ) {
      errors.address_pool_size = `Use a prefix between ${prefix} and 32.`;
    }
  }
  return errors;
};

const DockerDaemonSettingsSection = () => {
  const { privileged } = useAuth();
  const queryClient = useQueryClient();
  const { data: config, error: loadError } = useQuery(
    linuxio.docker.get_daemon_config,
  );
  const { data: dockerInfo } = useQuery(linuxio.docker.get_docker_info);
  const {
    draft,
    errors,
    setErrors,
    restartRequired,
    setRestartRequired,
    isDirty,
    patchKey,
    reset,
  } = useSettingsDraft<DockerDaemonConfig, DraftErrors>(config ?? null);

  const saveMutation = linuxio.docker.set_daemon_config.useMutation({
    success: (result) => {
      queryClient.setQueryData(
        linuxio.docker.get_daemon_config.queryKey,
        result.config,
      );
      reset();
      setRestartRequired(result.restart_required);
      toast.success(
        result.restart_required
          ? "Docker daemon settings saved. Restart Docker to apply them."
          : "Docker daemon settings applied.",
      );
    },
    error: "Failed to save Docker daemon settings",
  });
  const restartMutation = linuxio.systemd.restart_service.useMutation({
    success: () => {
      setRestartRequired(false);
      toast.success("Docker restarted.");
    },
    error: "Failed to restart Docker",
  });
  const [confirmStorage, setConfirmStorage] = useState(false);
  const [storageStep, setStorageStep] = useState<string | null>(null);
  const storageMutation =
    linuxio.docker.apply_daemon_storage.useTaskStreamAction<
      DockerDaemonConfigSetResult,
      TaskProgress<DockerDaemonStorageProgress>
    >({
      closeMessage: "Docker storage change stream closed unexpectedly",
      onProgress: (progress) => setStorageStep(progress.message ?? null),
      success: (result) => {
        queryClient.setQueryData(
          linuxio.docker.get_daemon_config.queryKey,
          result.config,
        );
        reset();
        setRestartRequired(false);
        toast.success(
          "Docker restarted on the new storage. Deploy your stacks again.",
        );
      },
      error: "Failed to change Docker storage",
      options: {
        onSettled: () => {
          setStorageStep(null);
          setConfirmStorage(false);
        },
      },
    });
  const busy =
    saveMutation.isPending ||
    restartMutation.isPending ||
    storageMutation.isPending;
  const disabled = !privileged || busy;

  const handleSave = () => {
    if (!draft || !config) return;
    const nextErrors = validateDraft(draft);
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) return;
    // Storage only changes with Docker stopped, which needs the user's go-ahead.
    if (
      trimDataRoot(draft.data_root) !== config.data_root ||
      draft.image_store !== config.image_store ||
      draft.storage_driver !== config.storage_driver
    ) {
      setConfirmStorage(true);
      return;
    }
    saveMutation.mutate(toRequest(draft));
  };

  if (loadError && !config) {
    return (
      <AppAlert severity="error">
        <AppAlertTitle>Docker daemon settings unavailable</AppAlertTitle>
        {loadError.message}
      </AppAlert>
    );
  }
  if (!draft) return <ComponentLoader />;

  // Automatic follows the running daemon; unknown while Docker is down.
  const containerdStore =
    draft.image_store === ""
      ? dockerInfo?.containerd_image_store
      : draft.image_store === "containerd";
  const drivers =
    containerdStore == null
      ? [...new Set([...CLASSIC_DRIVERS, ...CONTAINERD_SNAPSHOTTERS])]
      : containerdStore
        ? CONTAINERD_SNAPSHOTTERS
        : CLASSIC_DRIVERS;
  const dataRootChanging = trimDataRoot(draft.data_root) !== config?.data_root;

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-6)",
      }}
    >
      <div>
        <AppTypography fontWeight={600} variant="body1">
          Docker Daemon
        </AppTypography>
        <AppTypography color="text.secondary" variant="caption">
          {privileged
            ? "Edits /etc/docker/daemon.json. Other keys in the file are kept."
            : "Privileged mode is required to change Docker daemon settings."}
        </AppTypography>
      </div>

      {restartRequired ? (
        <AppAlert severity="info">
          <AppAlertTitle>Restart required</AppAlertTitle>
          Log limits, address pools and the userland proxy only apply after
          Docker restarts. With live restore on, running containers keep running
          through the restart.
          <div style={{ marginTop: "var(--app-space-4)" }}>
            <AppButton
              disabled={busy}
              onClick={() =>
                restartMutation.mutate({ serviceName: "docker.service" })
              }
              size="small"
              startIcon={<Icon height={16} icon="mdi:restart" width={16} />}
              variant="outlined"
            >
              {restartMutation.isPending ? "Restarting..." : "Restart Docker"}
            </AppButton>
          </div>
        </AppAlert>
      ) : null}

      <ToggleCard
        checked={draft.live_restore}
        description="Keep containers running while the daemon restarts or updates."
        disabled={disabled}
        label="Live restore"
        onChange={(checked) => patchKey("live_restore", checked)}
      />
      <ToggleCard
        checked={draft.userland_proxy}
        description="Off saves a docker-proxy process per published port, but host loopback stops reaching them."
        disabled={disabled}
        label="Userland proxy"
        onChange={(checked) => patchKey("userland_proxy", checked)}
      />

      <SectionCard
        icon="mdi:harddisk"
        subtitle="Where Docker keeps images, containers and volumes."
        title="Storage"
      >
        <SettingsGrid>
          <PathPickerField
            disabled={disabled}
            editable
            error={Boolean(errors.data_root)}
            helperText={errors.data_root ?? "Empty keeps the Docker default."}
            label="Data root"
            onChange={(value) => patchKey("data_root", value)}
            placeholder="/var/lib/docker"
            value={draft.data_root}
          />
          <AppSelect
            disabled={disabled}
            fullWidth
            label="Image store"
            onChange={(event) => {
              patchKey("image_store", event.target.value);
              // Overlay and plain-copy driver names differ between the stores.
              patchKey("storage_driver", "");
            }}
            size="small"
            value={draft.image_store}
          >
            <option value="">
              {dockerInfo
                ? `Automatic (now ${dockerInfo.containerd_image_store ? "containerd" : "classic"})`
                : "Automatic"}
            </option>
            <option value="classic">Classic (all in the data root)</option>
            <option value="containerd">containerd</option>
          </AppSelect>
          <AppSelect
            disabled={disabled}
            fullWidth
            label="Storage driver"
            onChange={(event) => patchKey("storage_driver", event.target.value)}
            size="small"
            value={draft.storage_driver}
          >
            <option value="">
              {dockerInfo?.storage_driver
                ? `Automatic (now ${dockerInfo.storage_driver})`
                : "Automatic"}
            </option>
            {/* A driver set by hand in daemon.json stays visible. */}
            {draft.storage_driver && !drivers.includes(draft.storage_driver) ? (
              <option value={draft.storage_driver}>
                {draft.storage_driver}
              </option>
            ) : null}
            {drivers.map((driver) => (
              <option key={driver} value={driver}>
                {driver}
              </option>
            ))}
          </AppSelect>
        </SettingsGrid>
        <SettingsNote title="Saving stops Docker">
          Changing any of these stops every container and Docker itself. Images,
          containers and volumes in the old location are hidden, not moved or
          deleted. The btrfs and zfs drivers need the data root on that
          filesystem.
        </SettingsNote>
        {draft.image_store !== "classic" &&
        (containerdStore || dataRootChanging) ? (
          <SettingsNote title="Images may stay in /var/lib/containerd">
            Only the classic image store keeps images in the data root. The
            containerd store keeps images and container layers in
            containerd&apos;s root, and Docker 29+ picks it on Automatic for an
            empty data root. Choose Classic to keep everything on the pool.
          </SettingsNote>
        ) : null}
      </SectionCard>

      <SectionCard
        icon="mdi:text-box-outline"
        subtitle="Caps json-file logs for containers created after the change."
        title="Container logs"
      >
        <SettingsGrid>
          <AppTextField
            disabled={disabled}
            error={Boolean(errors.log_max_size)}
            fullWidth
            helperText={
              errors.log_max_size ??
              "Per log file. Empty keeps the Docker default of no limit."
            }
            label="Max size"
            onChange={(event) => patchKey("log_max_size", event.target.value)}
            placeholder="10m"
            shrinkLabel
            size="small"
            value={draft.log_max_size}
          />
          <AppTextField
            disabled={disabled}
            error={Boolean(errors.log_max_file)}
            fullWidth
            helperText={
              errors.log_max_file ?? "Rotated files kept per container."
            }
            label="Max files"
            onChange={(event) => patchKey("log_max_file", event.target.value)}
            placeholder="3"
            shrinkLabel
            size="small"
            value={draft.log_max_file}
          />
        </SettingsGrid>
      </SectionCard>

      <SectionCard
        icon="mdi:ip-network-outline"
        subtitle="Subnets handed to new user-defined networks."
        title="Default address pool"
      >
        <SettingsGrid>
          <AppTextField
            disabled={disabled}
            error={Boolean(errors.address_pool_base)}
            fullWidth
            helperText={
              errors.address_pool_base ??
              "Empty keeps the Docker default of 172.17.0.0/12."
            }
            label="Base"
            onChange={(event) =>
              patchKey("address_pool_base", event.target.value)
            }
            placeholder="172.20.0.0/16"
            shrinkLabel
            size="small"
            value={draft.address_pool_base}
          />
          <AppTextField
            disabled={disabled}
            error={Boolean(errors.address_pool_size)}
            fullWidth
            helperText={
              errors.address_pool_size ??
              "Prefix length of each network carved from the base."
            }
            label="Network size"
            onChange={(event) =>
              patchKey("address_pool_size", Number(event.target.value) || 0)
            }
            placeholder="24"
            shrinkLabel
            size="small"
            type="number"
            value={draft.address_pool_size || ""}
          />
        </SettingsGrid>
        <SettingsNote title="Existing networks are not changed">
          A new pool only affects networks created afterwards. Recreating the
          network of a stack moves it to the new range.
        </SettingsNote>
      </SectionCard>

      <ConfirmDialog
        cancelText="Cancel"
        confirmText="Stop Docker and apply"
        destructive
        isPending={storageMutation.isPending}
        message="Every running container stops, then Docker stops, switches to the new storage and starts again. Anything in the old location is kept but hidden. Deploy your stacks again afterwards."
        onClose={() => {
          if (!storageMutation.isPending) setConfirmStorage(false);
        }}
        onConfirm={() => {
          if (draft) storageMutation.mutate(toRequest(draft));
        }}
        open={confirmStorage}
        pendingText="Applying…"
        progress={storageStep ? { message: storageStep } : null}
        title="Stop Docker to change storage?"
      />

      <SettingsSaveFooter
        busy={busy}
        isDirty={isDirty}
        onReset={reset}
        onSave={handleSave}
        saveDisabled={!privileged}
        saving={saveMutation.isPending}
      />
    </div>
  );
};

export default DockerDaemonSettingsSection;
