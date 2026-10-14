import { Icon } from "@iconify/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { linuxio, type DockerDaemonConfig, useCallMutation } from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";

import {
  SettingsGrid,
  SettingsSaveFooter,
  useSettingsDraft,
} from "./SettingsSectionForm";
import {
  SectionCard,
  SettingsNote,
  ToggleCard,
} from "./SettingsSectionPrimitives";

interface DraftErrors {
  log_max_size?: string;
  log_max_file?: string;
  address_pool_base?: string;
  address_pool_size?: string;
}

const validateDraft = (draft: DockerDaemonConfig): DraftErrors => {
  const errors: DraftErrors = {};
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

  const saveMutation = useCallMutation(linuxio.docker.set_daemon_config, {
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
  const restartMutation = useCallMutation(linuxio.systemd.restart_service, {
    success: () => {
      setRestartRequired(false);
      toast.success("Docker restarted.");
    },
    error: "Failed to restart Docker",
  });
  const busy = saveMutation.isPending || restartMutation.isPending;
  const disabled = !privileged || busy;

  const handleSave = () => {
    if (!draft) return;
    const nextErrors = validateDraft(draft);
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) return;
    saveMutation.mutate({
      ...draft,
      log_max_size: draft.log_max_size.trim(),
      log_max_file: draft.log_max_file.trim(),
      address_pool_base: draft.address_pool_base.trim(),
    });
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
