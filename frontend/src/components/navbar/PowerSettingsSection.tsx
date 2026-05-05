import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useMemo, useState } from "react";
import { toast } from "sonner";

import "./power-settings.css";

import { linuxio, type PowerStatus } from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSelect from "@/components/ui/AppSelect";
import AppTooltip from "@/components/ui/AppTooltip";
import { getMutationErrorMessage } from "@/utils/mutations";

const setPowerStatusCache = (
  queryClient: ReturnType<typeof useQueryClient>,
  status: PowerStatus,
) => {
  queryClient.setQueryData(linuxio.power.get_status.queryKey(), status);
};

const profileExists = (status: PowerStatus, profile: string) =>
  status.profiles.some((item) => item.name === profile);

const preferredProfile = (status: PowerStatus) => {
  if (status.active_profile && profileExists(status, status.active_profile)) {
    return status.active_profile;
  }
  if (
    status.recommended_profile &&
    profileExists(status, status.recommended_profile)
  ) {
    return status.recommended_profile;
  }
  return status.profiles[0]?.name ?? "";
};

const PowerBadge: React.FC<{
  label: string;
  tone: "info" | "success" | "warning" | "error";
}> = ({ label, tone }) => (
  <span className={`power-settings-badge power-settings-badge--${tone}`}>
    {label}
  </span>
);

const StatusBadge: React.FC<{ status: PowerStatus }> = ({ status }) => {
  if (!status.tuned_available) {
    return <PowerBadge label="Unavailable" tone="warning" />;
  }
  if (!status.tuned_active) {
    return <PowerBadge label="Stopped" tone="error" />;
  }
  return <PowerBadge label="Running" tone="success" />;
};

const InfoMetric: React.FC<{ label: string; value: React.ReactNode }> = ({
  label,
  value,
}) => (
  <div className="power-settings-metric">
    <span className="power-settings-metric__label">{label}</span>
    <span className="power-settings-metric__value">{value}</span>
  </div>
);

const PowerSettingsSection: React.FC = () => {
  const queryClient = useQueryClient();
  const [selectedProfile, setSelectedProfile] = useState("");
  const {
    data: status,
    isPending,
    error,
  } = linuxio.power.get_status.useQuery({
    refetchInterval: 15000,
  });

  const commonMutationOptions = {
    onSuccess: (nextStatus: PowerStatus) => {
      setPowerStatusCache(queryClient, nextStatus);
    },
    onError: (err: Error) => {
      toast.error(getMutationErrorMessage(err, "Power action failed"));
    },
  };

  const startMutation = linuxio.power.start.useMutation({
    ...commonMutationOptions,
    onSuccess: (nextStatus) => {
      commonMutationOptions.onSuccess(nextStatus);
      toast.success("TuneD started");
    },
  });

  const setProfileMutation = linuxio.power.set_profile.useMutation({
    ...commonMutationOptions,
    onSuccess: (nextStatus) => {
      commonMutationOptions.onSuccess(nextStatus);
      toast.success("Power profile applied");
    },
  });

  const disableMutation = linuxio.power.disable.useMutation({
    ...commonMutationOptions,
    onSuccess: (nextStatus) => {
      commonMutationOptions.onSuccess(nextStatus);
      toast.success("TuneD tunings disabled");
    },
  });

  const resolvedProfile = useMemo(() => {
    if (!status) return selectedProfile;
    if (selectedProfile && profileExists(status, selectedProfile))
      return selectedProfile;
    return preferredProfile(status);
  }, [selectedProfile, status]);

  const selectedProfileDetails = useMemo(
    () => status?.profiles.find((profile) => profile.name === resolvedProfile),
    [resolvedProfile, status?.profiles],
  );

  if (isPending) {
    return (
      <div className="power-settings__loading">
        <ComponentLoader />
      </div>
    );
  }

  if (error || !status) {
    return (
      <AppAlert severity="error">
        <AppAlertTitle>Power status unavailable</AppAlertTitle>
        {error?.message || "LinuxIO could not read power management status."}
      </AppAlert>
    );
  }

  const busy =
    startMutation.isPending ||
    setProfileMutation.isPending ||
    disableMutation.isPending;
  const canControlTuned =
    status.tuned_available && (status.tuned_active || status.tuned_startable);
  const selectedIsActive =
    Boolean(resolvedProfile) && resolvedProfile === status.active_profile;
  const canApplyProfile =
    canControlTuned && Boolean(resolvedProfile) && !selectedIsActive && !busy;
  let profileActionLabel = "Apply";
  if (setProfileMutation.isPending) {
    profileActionLabel = "Applying...";
  } else if (selectedIsActive) {
    profileActionLabel = "Applied";
  }

  return (
    <div className="power-settings" aria-busy={busy}>
      {!status.tuned_available ? (
        <AppAlert severity="warning">
          <AppAlertTitle>TuneD unavailable</AppAlertTitle>
          Install TuneD with <code>{status.install_command}</code>.
        </AppAlert>
      ) : null}

      {status.power_profiles_daemon_active ? (
        <AppAlert severity="warning">
          <AppAlertTitle>Conflicting daemon active</AppAlertTitle>
          power-profiles-daemon may override TuneD profile changes.
        </AppAlert>
      ) : null}

      {status.error ? (
        <AppAlert severity="warning">
          <AppAlertTitle>Partial TuneD status</AppAlertTitle>
          {status.error}
        </AppAlert>
      ) : null}

      <section className="power-settings__section">
        <div className="power-settings__section-header">
          <div className="power-settings__status-left">
            <h3 className="power-settings__section-title">Status</h3>
            <StatusBadge status={status} />
          </div>
          <AppTooltip
            title={
              !status.tuned_available
                ? ""
                : status.tuned_active
                  ? "Turn Off"
                  : status.tuned_startable
                    ? "Start TuneD"
                    : "TuneD cannot be started automatically"
            }
          >
            <AppIconButton
              style={{
                color: status.tuned_active
                  ? "var(--app-palette-success-main)"
                  : status.tuned_available && status.tuned_startable
                    ? "var(--app-palette-error-main)"
                    : "var(--app-palette-text-disabled)",
              }}
              disabled={
                busy ||
                !status.tuned_available ||
                (!status.tuned_active && !status.tuned_startable)
              }
              aria-label={
                status.tuned_active ? "Turn Off TuneD" : "Start TuneD"
              }
              onClick={() =>
                status.tuned_active
                  ? disableMutation.mutate([])
                  : startMutation.mutate([])
              }
            >
              <Icon icon="mdi:power" width={22} height={22} />
            </AppIconButton>
          </AppTooltip>
        </div>
        <div className="power-settings__metrics">
          <InfoMetric
            label="Active profile"
            value={status.active_profile || "None"}
          />
          <InfoMetric
            label="Recommended"
            value={status.recommended_profile || "Unknown"}
          />
          <InfoMetric
            label="Start path"
            value={status.tuned_startable ? "Available" : "Unavailable"}
          />
          <InfoMetric
            label="Unit file"
            value={status.tuned_unit_file_state || "Unknown"}
          />
          <InfoMetric label="Profiles" value={String(status.profiles.length)} />
        </div>
      </section>

      <section className="power-settings__section">
        <div className="power-settings__section-header">
          <h3 className="power-settings__section-title">Profile</h3>
        </div>
        <div className="power-settings__profile-control">
          <span className="power-settings__label power-settings__label--span">
            Available profiles
          </span>
          <AppSelect
            fullWidth
            size="small"
            value={resolvedProfile}
            disabled={busy || status.profiles.length === 0}
            onChange={(event) => setSelectedProfile(event.target.value)}
            renderOption={(value) => {
              const profile = status.profiles.find((p) => p.name === value);
              return (
                <>
                  <span>{value}</span>
                  {profile?.active && (
                    <PowerBadge label="Active" tone="success" />
                  )}
                  {profile?.recommended && (
                    <PowerBadge label="Recommended" tone="info" />
                  )}
                </>
              );
            }}
            renderValue={(value) => {
              const profile = status.profiles.find((p) => p.name === value);
              return (
                <>
                  <span>{value}</span>
                  {profile?.active && (
                    <PowerBadge label="Active" tone="success" />
                  )}
                  {profile?.recommended && (
                    <PowerBadge label="Recommended" tone="info" />
                  )}
                </>
              );
            }}
          >
            {status.profiles.length === 0 ? (
              <option value="">No profiles reported</option>
            ) : (
              status.profiles.map((profile) => (
                <option key={profile.name} value={profile.name}>
                  {profile.name}
                </option>
              ))
            )}
          </AppSelect>
          <AppButton
            size="small"
            variant={selectedIsActive ? "text" : "contained"}
            disabled={!canApplyProfile}
            startIcon={<Icon icon="mdi:check" width={18} height={18} />}
            onClick={() => setProfileMutation.mutate([resolvedProfile])}
          >
            {profileActionLabel}
          </AppButton>
        </div>

        {selectedProfileDetails ? (
          <div className="power-settings__profile-details">
            {selectedProfileDetails.description ? (
              <p className="power-settings__muted">
                {selectedProfileDetails.description}
              </p>
            ) : null}
          </div>
        ) : (
          <p className="power-settings__muted">No TuneD profiles reported.</p>
        )}
      </section>
    </div>
  );
};

export default PowerSettingsSection;
