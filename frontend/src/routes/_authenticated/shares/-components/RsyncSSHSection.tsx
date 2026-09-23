import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";

import { linuxio } from "@/api";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import PathPickerField from "@/components/ui/PathPickerField";
import useAuth from "@/hooks/useAuth";

export default function RsyncSSHSection({
  modulePath,
}: {
  modulePath?: string;
}) {
  const { user } = useAuth();
  const [username, setUsername] = useState(user?.id ?? "");
  const [path, setPath] = useState<string | null>(null);
  const [portInput, setPortInput] = useState("22");
  const [checkedPort, setCheckedPort] = useState(22);
  const port = Number(portInput);
  const validPort = Number.isInteger(port) && port >= 1 && port <= 65535;
  const {
    data: ssh,
    error,
    isFetching,
    refetch,
  } = useQuery(linuxio.shares.get_rsync_ssh({ port: checkedPort }));
  const source = path ?? modulePath ?? "";
  return (
    <section
      className="rsync-page__connection"
      aria-label="SSH backup connection"
    >
      <AppTypography component="h2" variant="h6">
        SSH
      </AppTypography>
      <AppTypography variant="body2">
        Use the existing SSH service for encrypted backups. In TOS, select rsync
        over SSH, enter this server’s LAN IP and the Linux account’s SSH
        credentials, then choose the source folder below.
      </AppTypography>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (!validPort) return;
          if (port === checkedPort) void refetch();
          else setCheckedPort(port);
        }}
      >
        <AppTextField
          label="SSH port"
          type="number"
          required
          value={portInput}
          error={!validPort}
          onChange={(event) => setPortInput(event.target.value)}
          helperText={
            validPort
              ? "Default: 22. Enter the port used by your existing SSH service. This does not change sshd."
              : "Enter a port between 1 and 65535."
          }
        />
        <AppButton
          type="submit"
          disabled={isFetching || !validPort}
          variant="outlined"
        >
          {isFetching ? "Checking SSH…" : `Check SSH port ${portInput}`}
        </AppButton>
      </form>
      {port === checkedPort && (ssh || error) ? (
        <AppAlert severity={error || !ssh?.available ? "warning" : "success"}>
          {error?.message ??
            ssh?.error ??
            "The local SSH listener responds. Verify the account and folder access from TOS."}
        </AppAlert>
      ) : (
        <AppAlert severity="info">
          Check the selected SSH port to verify the local listener.
        </AppAlert>
      )}
      <AppTextField
        label="Linux username for SSH"
        autoComplete="off"
        value={username}
        onChange={(event) => setUsername(event.target.value)}
        helperText="Use a Linux account with read access to the source folder."
      />
      <PathPickerField
        editable
        label="SSH source folder"
        value={source}
        onChange={setPath}
      />
      <dl>
        <dt>Mode</dt>
        <dd>rsync over SSH</dd>
        <dt>Port</dt>
        <dd>{validPort ? port : "Choose a valid port"}</dd>
        <dt>Username</dt>
        <dd>{username || "Choose a Linux account"}</dd>
        <dt>Source path</dt>
        <dd>{source || "Choose a source folder"}</dd>
        <dt>Authentication</dt>
        <dd>The Linux account’s SSH credentials</dd>
      </dl>
      <AppTypography color="text.secondary" variant="body2">
        SSH uses Linux file permissions. The module’s password, read-only
        protection and NAS IP restriction apply only to module mode. The rsync
        module can remain stopped when using SSH.
      </AppTypography>
      <AppTypography color="text.secondary" variant="body2">
        Allow the TNAS to reach TCP port{" "}
        {validPort ? port : "Choose a valid port"}. Manage Linux users in{" "}
        <Link to="/accounts">Accounts</Link> and the existing SSH service in{" "}
        <Link to="/services">Services</Link>. The local check does not verify
        the firewall or credentials from the NAS.
      </AppTypography>
    </section>
  );
}
