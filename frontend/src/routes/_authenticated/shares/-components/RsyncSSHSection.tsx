import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type SubmitEvent } from "react";

import { linuxio, type RsyncSSHConfig, useCallMutation } from "@/api";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import PathPickerField from "@/components/ui/PathPickerField";
import useAuth from "@/hooks/useAuth";
import { SettingsGrid } from "@/routes/_authenticated/-components/navbar/SettingsSectionForm";
import {
  SectionCard,
  StatusGroupLabel,
} from "@/routes/_authenticated/-components/navbar/SettingsSectionPrimitives";

function RsyncSSHForm({
  config,
  modulePath,
  onError,
}: {
  config?: RsyncSSHConfig;
  modulePath?: string;
  onError: (message: string) => void;
}) {
  const { user } = useAuth();
  const [username, setUsername] = useState(config?.username ?? user?.id ?? "");
  const [path, setPath] = useState(config?.path ?? modulePath ?? "");
  const failed = (failure: Error) => onError(failure.message);
  const save = useCallMutation(linuxio.shares.save_rsync_ssh, {
    error: failed,
  });
  const remove = useCallMutation(linuxio.shares.remove_rsync_ssh, {
    error: failed,
  });
  const pending = save.isPending || remove.isPending;
  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    onError("");
    save.mutate({ username, path });
  };
  return (
    <form className="rsync-page__form" onSubmit={submit}>
      <SettingsGrid>
        <AppTextField
          label="Linux account for SSH"
          size="small"
          fullWidth
          required
          autoComplete="off"
          value={username}
          disabled={pending}
          onChange={(event) => setUsername(event.target.value)}
          helperText="Any regular account with read access to the source folder. TOS stores its SSH password, so a dedicated account limits what a compromised NAS could read."
        />
      </SettingsGrid>
      <PathPickerField
        editable
        required
        label="SSH source folder"
        value={path}
        disabled={pending}
        onChange={setPath}
      />
      <div className="rsync-page__actions">
        <AppButton
          type="submit"
          variant="contained"
          disabled={pending}
          keepTextOnMobile
          startIcon={<Icon icon="mdi:content-save-outline" width={18} />}
        >
          {save.isPending ? "Saving…" : "Save SSH module"}
        </AppButton>
        <AppButton
          variant="outlined"
          color="inherit"
          keepTextOnMobile
          startIcon={<Icon icon="mdi:delete-outline" width={18} />}
          disabled={pending || !config}
          onClick={() => {
            onError("");
            remove.mutate();
          }}
        >
          Remove
        </AppButton>
      </div>
    </form>
  );
}

export default function RsyncSSHSection({
  modulePath,
}: {
  modulePath?: string;
}) {
  const [error, setError] = useState("");
  const [portInput, setPortInput] = useState("22");
  const [checkedPort, setCheckedPort] = useState(22);
  const port = Number(portInput);
  const validPort = Number.isInteger(port) && port >= 1 && port <= 65535;
  const {
    data: ssh,
    error: checkError,
    isFetching,
    refetch,
  } = useQuery(linuxio.shares.get_rsync_ssh({ port: checkedPort }));
  const checked = port === checkedPort && Boolean(ssh || checkError);
  const available = checked && ssh?.available && !checkError;
  const config = ssh?.config;
  return (
    <section aria-label="SSH backup connection">
      <SectionCard
        headingComponent="h2"
        icon="mdi:shield-lock-outline"
        title="SSH"
        subtitle="Encrypted transfers through a Linux account"
        titleAdornment={
          <AppChip
            size="small"
            variant="soft"
            color={checked ? (available ? "success" : "warning") : "default"}
            label={
              isFetching
                ? "Checking…"
                : checked
                  ? available
                    ? "Available"
                    : "Unavailable"
                  : "Not checked"
            }
          />
        }
      >
        <div className="rsync-page__stack">
          <AppTypography color="text.secondary" variant="caption">
            TOS logs in over SSH and starts rsync as the chosen account, which
            serves the read-only module saved here from its home folder. The
            module daemon on port 873 can stay stopped.
          </AppTypography>
          <form
            className="rsync-page__form"
            onSubmit={(event) => {
              event.preventDefault();
              if (!validPort) return;
              if (port === checkedPort) void refetch();
              else setCheckedPort(port);
            }}
          >
            <SettingsGrid>
              <AppTextField
                label="SSH port"
                size="small"
                fullWidth
                type="number"
                required
                value={portInput}
                error={!validPort}
                onChange={(event) => setPortInput(event.target.value)}
                helperText={
                  validPort
                    ? "Default: 22. Use your server’s SSH port."
                    : "Enter a port between 1 and 65535."
                }
              />
            </SettingsGrid>
            <div className="rsync-page__actions">
              <AppButton
                type="submit"
                disabled={isFetching || !validPort}
                variant="outlined"
                keepTextOnMobile
                startIcon={<Icon icon="mdi:connection" width={18} />}
              >
                {isFetching ? "Checking SSH…" : `Check SSH port ${portInput}`}
              </AppButton>
            </div>
          </form>
          <div role="status">
            {checked && (checkError || !ssh?.available) ? (
              <AppAlert severity="warning">
                {checkError?.message ?? ssh?.error}
              </AppAlert>
            ) : (
              <AppTypography color="text.secondary" variant="caption">
                {checked
                  ? "The local SSH listener responds. Verify access from TOS."
                  : "Check the selected SSH port to verify the local listener."}
              </AppTypography>
            )}
          </div>
          {error && <AppAlert severity="error">{error}</AppAlert>}
          <RsyncSSHForm
            key={JSON.stringify(config ?? null)}
            config={config}
            modulePath={modulePath}
            onError={setError}
          />
          {config && (
            <>
              <StatusGroupLabel>Connect from TOS</StatusGroupLabel>
              <div>
                <InfoRow label="Mode" wrap>
                  rsync over SSH
                </InfoRow>
                <InfoRow label="Port" wrap>
                  {validPort ? port : "Choose a valid port"}
                </InfoRow>
                <InfoRow label="Username" wrap>
                  {config.username}
                </InfoRow>
                <InfoRow label="Module" wrap>
                  {config.module}
                </InfoRow>
                <InfoRow label="Source path" wrap>
                  {config.path}
                </InfoRow>
              </div>
              <AppTypography color="text.secondary" variant="caption">
                In TOS, add this server’s LAN IP with the SSH port and the
                account’s SSH password, then pick the module from the backup
                source list. Access follows the account’s Linux permissions; the
                module’s NAS IP restriction does not apply.
              </AppTypography>
            </>
          )}
          <AppTypography color="text.secondary" variant="caption">
            Allow the selected TCP port from the TNAS. Manage users in{" "}
            <Link to="/accounts">Accounts</Link> and SSH in{" "}
            <Link to="/services">Services</Link>. The port check does not change
            SSH settings or test the NAS firewall and credentials.
          </AppTypography>
        </div>
      </SectionCard>
    </section>
  );
}
