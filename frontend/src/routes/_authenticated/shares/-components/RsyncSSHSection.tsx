import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";

import { linuxio, type RsyncStatus } from "@/api";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import { SettingsGrid } from "@/routes/_authenticated/-components/navbar/SettingsSectionForm";
import {
  SectionCard,
  StatusGroupLabel,
} from "@/routes/_authenticated/-components/navbar/SettingsSectionPrimitives";

export default function RsyncSSHSection({ status }: { status: RsyncStatus }) {
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
  const checked = port === checkedPort && Boolean(ssh || error);
  const available = checked && ssh?.available && !error;
  const config = status.config;
  return (
    <section aria-label="SSH backup connection">
      <SectionCard
        headingComponent="h2"
        icon="mdi:shield-lock-outline"
        title="SSH"
        subtitle="TOS encryption mode through a Linux account"
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
            TOS logs in over SSH with a Linux account, checks that the module
            daemon is running, reads the module folder from /etc/rsyncd.conf and
            copies it with rsync over SSH as that account.
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
          {config ? (
            <>
              {!status.ssh_ready && (
                <AppAlert severity="warning">
                  {status.ssh_error ??
                    "The module is stopped. TOS checks for a running rsync daemon before it copies over SSH."}
                </AppAlert>
              )}
              <StatusGroupLabel>Connect from TOS</StatusGroupLabel>
              <div>
                <InfoRow label="Mode" wrap>
                  rsync module encryption mode
                </InfoRow>
                <InfoRow label="Port" wrap>
                  {validPort ? port : "Choose a valid port"}
                </InfoRow>
                <InfoRow label="Username" wrap>
                  A Linux account that can read {config.path}
                </InfoRow>
                <InfoRow label="Password" wrap>
                  That account’s SSH password
                </InfoRow>
                <InfoRow label="Module" wrap>
                  {config.module}
                </InfoRow>
              </div>
              <AppTypography color="text.secondary" variant="caption">
                Files are copied with the account’s Linux permissions, so a
                restore from TOS can write where that account can. The module
                password and NAS IP restriction apply only to port {config.port}
                . Keep the module running.
              </AppTypography>
            </>
          ) : (
            <AppTypography color="text.secondary" variant="caption">
              Save and start the module first. TOS’s encryption mode uses the
              same module through SSH.
            </AppTypography>
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
