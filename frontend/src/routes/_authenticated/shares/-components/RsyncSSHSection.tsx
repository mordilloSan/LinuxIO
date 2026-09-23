import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";

import { linuxio } from "@/api";
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
  const checked = port === checkedPort && Boolean(ssh || error);
  return (
    <section aria-label="SSH backup connection">
      <SectionCard
        headingComponent="h2"
        icon="mdi:shield-lock-outline"
        title="SSH"
        subtitle="Encrypted transfers using a Linux account"
        titleAdornment={
          <AppChip
            size="small"
            variant="soft"
            color={
              checked
                ? ssh?.available && !error
                  ? "success"
                  : "warning"
                : "default"
            }
            label={
              isFetching
                ? "Checking…"
                : checked
                  ? ssh?.available && !error
                    ? "Available"
                    : "Unavailable"
                  : "Not checked"
            }
          />
        }
      >
        <div className="rsync-page__stack">
          <AppTypography color="text.secondary" variant="caption">
            Use the server’s existing SSH service. The rsync module can stay
            stopped when using this mode.
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
              <AppTextField
                label="Linux username for SSH"
                size="small"
                fullWidth
                autoComplete="off"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                helperText="Requires read access to the source folder."
              />
            </SettingsGrid>
            <PathPickerField
              editable
              label="SSH source folder"
              value={source}
              onChange={setPath}
            />
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
            {checked && (error || !ssh?.available) ? (
              <AppAlert severity="warning">
                {error?.message ?? ssh?.error}
              </AppAlert>
            ) : (
              <AppTypography color="text.secondary" variant="caption">
                {checked
                  ? "The local SSH listener responds. Verify access from TOS."
                  : "Check the selected SSH port to verify the local listener."}
              </AppTypography>
            )}
          </div>
          <StatusGroupLabel>Connect from TOS</StatusGroupLabel>
          <div>
            <InfoRow label="Mode" wrap>
              rsync over SSH
            </InfoRow>
            <InfoRow label="Port" wrap>
              {validPort ? port : "Choose a valid port"}
            </InfoRow>
            <InfoRow label="Username" wrap>
              {username || "Choose a Linux account"}
            </InfoRow>
            <InfoRow label="Source path" wrap>
              {source || "Choose a source folder"}
            </InfoRow>
          </div>
          <AppTypography color="text.secondary" variant="caption">
            Use this server’s LAN IP and the Linux account’s SSH credentials in
            TOS. Access follows Linux permissions; the module’s read-only and
            NAS IP restrictions do not apply.
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Allow the selected TCP port from the TNAS. Manage users in{" "}
            <Link to="/accounts">Accounts</Link> and SSH in{" "}
            <Link to="/services">Services</Link>. This check does not change SSH
            settings or test the NAS firewall and credentials.
          </AppTypography>
        </div>
      </SectionCard>
    </section>
  );
}
