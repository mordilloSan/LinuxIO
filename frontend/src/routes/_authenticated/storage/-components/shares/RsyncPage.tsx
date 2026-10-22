import { Icon } from "@iconify/react";
import { useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type SubmitEvent } from "react";

import {
  invalidateOperationQueries,
  linuxio,
  type RsyncConfig,
  type RsyncStatus,
  useCallMutation,
} from "@/api";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import PathPickerField from "@/components/ui/PathPickerField";
import { useCapability } from "@/hooks/useCapabilities";
import { SettingsGrid } from "@/routes/_authenticated/-components/navbar/SettingsSectionForm";
import {
  SectionCard,
  StatusGroupLabel,
} from "@/routes/_authenticated/-components/navbar/SettingsSectionPrimitives";

import RsyncSSHSection from "./RsyncSSHSection";

import "./rsync-page.css";

function RsyncForm({
  status,
  onError,
}: {
  status: RsyncStatus;
  onError: (message: string) => void;
}) {
  const queryClient = useQueryClient();
  const [config, setConfig] = useState<RsyncConfig>(
    status.config ?? {
      module: "backup",
      path: "",
      username: "tnas",
      nas_address: "",
      port: 873,
    },
  );
  const [password, setPassword] = useState("");
  const updateStatus = (next: RsyncStatus) => {
    queryClient.setQueryData(linuxio.shares.get_rsync.queryKey, next);
  };
  const handleFailure = (failure: Error) => {
    onError(failure.message);
    // A failed save can still have stopped the daemon or written its config.
    void invalidateOperationQueries(
      queryClient,
      linuxio.shares.save_rsync.route,
    );
  };
  const save = useCallMutation(linuxio.shares.save_rsync, {
    success: (next) => {
      updateStatus(next);
      setPassword("");
    },
    error: handleFailure,
    options: { gcTime: 0 },
  });
  const stop = useCallMutation(linuxio.shares.stop_rsync, {
    success: updateStatus,
    error: handleFailure,
  });
  const pending = save.isPending || stop.isPending;
  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    onError("");
    save.mutate({ ...config, password }, { onSettled: () => save.reset() });
  };
  return (
    <form className="rsync-page__form" onSubmit={submit}>
      <AppTypography color="text.secondary" variant="caption">
        Export a folder for the TNAS to read, including root and container
        files. The NAS cannot change or delete files on this server.
      </AppTypography>
      <PathPickerField
        editable
        label="Backup folder"
        onChange={(path) => setConfig({ ...config, path })}
        required
        value={config.path}
        disabled={pending}
      />
      <SettingsGrid>
        <AppTextField
          size="small"
          fullWidth
          label="Module name"
          value={config.module}
          required
          disabled={pending}
          helperText="Use this name in TOS. It appears in the backup source list."
          onChange={(event) =>
            setConfig({ ...config, module: event.target.value })
          }
        />
        <AppTextField
          size="small"
          fullWidth
          label="Module port"
          helperText="Default: 873. Choose a free port."
          type="number"
          value={config.port}
          required
          disabled={pending}
          onChange={(event) =>
            setConfig({ ...config, port: Number(event.target.value) })
          }
        />
      </SettingsGrid>
      <StatusGroupLabel>NAS access</StatusGroupLabel>
      <AppTextField
        size="small"
        fullWidth
        label="TNAS IP address"
        value={config.nas_address}
        required
        disabled={pending}
        placeholder="192.168.1.249"
        helperText="Only this address and localhost can access the module."
        onChange={(event) =>
          setConfig({ ...config, nas_address: event.target.value })
        }
      />
      <SettingsGrid>
        <AppTextField
          size="small"
          fullWidth
          label="Backup username"
          value={config.username}
          required
          disabled={pending}
          autoComplete="off"
          helperText="Module credentials, separate from Linux accounts."
          onChange={(event) =>
            setConfig({ ...config, username: event.target.value })
          }
        />
        <AppTextField
          size="small"
          fullWidth
          label="Backup password"
          type="password"
          value={password}
          disabled={pending}
          required={
            !status.config || status.config.username !== config.username
          }
          autoComplete="new-password"
          helperText={
            status.config
              ? "Leave blank to keep it. New password: 12+ characters, no spaces."
              : "At least 12 characters, no spaces."
          }
          onChange={(event) => setPassword(event.target.value)}
        />
      </SettingsGrid>
      <div className="rsync-page__actions">
        <AppButton
          type="submit"
          variant="contained"
          disabled={pending}
          keepTextOnMobile
          startIcon={<Icon icon="mdi:play" width={18} />}
        >
          {save.isPending ? "Starting…" : "Save and start"}
        </AppButton>
        <AppButton
          variant="outlined"
          color="inherit"
          keepTextOnMobile
          startIcon={<Icon icon="mdi:stop" width={18} />}
          disabled={pending || (!status.active && !status.enabled)}
          onClick={() => {
            onError("");
            stop.mutate();
          }}
        >
          Stop and disable
        </AppButton>
      </div>
      <AppTypography color="text.secondary" variant="caption">
        Saving restarts the module and interrupts running backups. The module
        starts automatically after reboot.
      </AppTypography>
    </form>
  );
}

export default function RsyncPage() {
  const [error, setError] = useState("");
  const { data: status } = useSuspenseQuery({
    ...linuxio.shares.get_rsync,
    refetchInterval: 5000,
  });
  const { isEnabled, reason } = useCapability("rsyncAvailable");
  return (
    <div className="rsync-page">
      <header>
        <AppTypography component="h1" variant="subtitle1" fontWeight={600}>
          rsync backups
        </AppTypography>
        <AppTypography color="text.secondary" variant="caption">
          Let TerraMaster TOS pull files through a read-only module or SSH.
          Manage schedules and retention in TOS.
        </AppTypography>
      </header>
      <div className="rsync-page__modes">
        <section aria-label="Rsync module">
          <SectionCard
            headingComponent="h2"
            icon="mdi:folder-network-outline"
            title="Rsync module"
            subtitle="Read-only access restricted to your TNAS"
            titleAdornment={
              <AppChip
                label={status.active ? "Running" : "Stopped"}
                color={status.active ? "success" : "default"}
                size="small"
                variant="soft"
              />
            }
          >
            <div className="rsync-page__stack">
              {error && <AppAlert severity="error">{error}</AppAlert>}
              {!isEnabled ? (
                <AppAlert severity="info">
                  {reason}{" "}
                  <Link to="/settings" search={{ tab: "capabilities" }}>
                    Open Capabilities to install rsync
                  </Link>
                </AppAlert>
              ) : (
                <RsyncForm
                  key={JSON.stringify(status.config)}
                  status={status}
                  onError={setError}
                />
              )}
              {status.config && (
                <section
                  className="rsync-page__stack"
                  aria-label="TOS connection details"
                >
                  <StatusGroupLabel>Connect from TOS</StatusGroupLabel>
                  <div>
                    <InfoRow label="Mode" wrap>
                      rsync module
                    </InfoRow>
                    <InfoRow label="Port">{status.config.port}</InfoRow>
                    <InfoRow label="Module" wrap>
                      {status.config.module}
                    </InfoRow>
                    <InfoRow label="Username" wrap>
                      {status.config.username}
                    </InfoRow>
                  </div>
                  <AppTypography color="text.secondary" variant="caption">
                    In TOS Centralized Backup, add this server’s LAN IP and
                    these settings. Use the backup password saved above.
                  </AppTypography>
                  <AppTypography color="text.secondary" variant="caption">
                    Allow TCP port {status.config.port} from{" "}
                    {status.config.nas_address}. Module traffic is unencrypted;
                    use a trusted LAN or VPN.
                  </AppTypography>
                </section>
              )}
            </div>
          </SectionCard>
        </section>
        <RsyncSSHSection status={status} />
      </div>
    </div>
  );
}
