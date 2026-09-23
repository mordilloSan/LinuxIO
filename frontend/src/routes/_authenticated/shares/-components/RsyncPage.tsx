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
import PathPickerField from "@/components/ui/PathPickerField";
import { useCapability } from "@/hooks/useCapabilities";

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
      <AppTypography component="h2" variant="h6">
        rsync module — port {config.port}
      </AppTypography>
      <AppTypography color="text.secondary" variant="body2">
        The NAS can read this folder, including files owned by root and
        containers. It cannot upload, change or delete files on this server.
      </AppTypography>
      <PathPickerField
        editable
        label="Backup folder"
        onChange={(path) => setConfig({ ...config, path })}
        required
        value={config.path}
        disabled={pending}
      />
      <AppTextField
        label="Module name"
        value={config.module}
        required
        disabled={pending}
        helperText="Use this module name in TOS. The module is hidden from public listings."
        onChange={(event) =>
          setConfig({ ...config, module: event.target.value })
        }
      />
      <AppTextField
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
      <AppTextField
        label="Module port"
        helperText="873 is the default rsync module port. Choose a free port, separate from SSH."
        type="number"
        value={config.port}
        required
        disabled={pending}
        onChange={(event) =>
          setConfig({ ...config, port: Number(event.target.value) })
        }
      />
      <AppTextField
        label="Backup username"
        value={config.username}
        required
        disabled={pending}
        autoComplete="off"
        helperText="This belongs to the rsync module, independently of Linux accounts."
        onChange={(event) =>
          setConfig({ ...config, username: event.target.value })
        }
      />
      <AppTextField
        label="Backup password"
        type="password"
        value={password}
        disabled={pending}
        required={!status.config || status.config.username !== config.username}
        autoComplete="new-password"
        helperText={
          status.config
            ? "Leave blank to keep the password. A new password needs at least 12 characters without spaces."
            : "At least 12 characters without spaces. Use the same password in TOS."
        }
        onChange={(event) => setPassword(event.target.value)}
      />
      <div className="rsync-page__actions">
        <AppButton type="submit" variant="contained" disabled={pending}>
          {save.isPending ? "Starting…" : "Save and start"}
        </AppButton>
        <AppButton
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
        Saving restarts the daemon and interrupts any running backup. It also
        enables startup after reboot.
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
      <div className="rsync-page__heading">
        <AppTypography component="h1" variant="h5">
          rsync backups
        </AppTypography>
        <AppChip
          label={status.active ? "Running" : "Stopped"}
          color={status.active ? "success" : "default"}
        />
      </div>
      <AppTypography color="text.secondary" variant="body2">
        Let TerraMaster TOS pull backups using a read-only rsync module on port
        873 or the existing SSH service.
      </AppTypography>
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
          className="rsync-page__connection"
          aria-label="TOS connection details"
        >
          <AppTypography component="h2" variant="h6">
            Connect from TOS
          </AppTypography>
          <AppTypography variant="body2">
            In Centralized Backup, add an rsync file server using this Linux
            server’s LAN IP and the settings below.
          </AppTypography>
          <dl>
            <dt>Mode</dt>
            <dd>rsync module</dd>
            <dt>Port</dt>
            <dd>{status.config.port}</dd>
            <dt>Module</dt>
            <dd>{status.config.module}</dd>
            <dt>Username</dt>
            <dd>{status.config.username}</dd>
            <dt>Password</dt>
            <dd>The backup password you saved above</dd>
          </dl>
          <AppTypography color="text.secondary" variant="body2">
            Allow TCP port {status.config.port} from {status.config.nas_address}{" "}
            in your server firewall. Module traffic is unencrypted; use a
            trusted LAN or VPN. Set the schedule and retention in TOS.
          </AppTypography>
        </section>
      )}
      <RsyncSSHSection modulePath={status.config?.path} />
    </div>
  );
}
