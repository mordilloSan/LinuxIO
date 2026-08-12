import { Icon } from "@iconify/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";
import { toast } from "sonner";

import "./power-settings.css";

import { linuxio, type PowerStatus, useCallMutation } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";

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

const PowerBadge = ({
  label,
  tone,
}: {
  label: string;
  tone: "info" | "success" | "warning" | "error";
}) => (
  <span className={`power-settings-badge power-settings-badge--${tone}`}>
    {label}
  </span>
);

const StatusBadge = ({ status }: { status: PowerStatus }) => {
  if (!status.tuned_available) {
    return <PowerBadge label="Unavailable" tone="warning" />;
  }
  if (!status.tuned_active) {
    return <PowerBadge label="Stopped" tone="error" />;
  }
  return <PowerBadge label="Running" tone="success" />;
};

const InfoMetric = ({ label, value }: { label: string; value: ReactNode }) => (
  <div className="power-settings-metric">
    <span className="power-settings-metric__label">{label}</span>
    <span className="power-settings-metric__value">{value}</span>
  </div>
);

const PowerSettingsSection = () => {
  const queryClient = useQueryClient();
  const powerStatusCache = {
    set: (value: unknown) =>
      queryClient.setQueryData(linuxio.power.get_status.queryKey, value),
  };
  const [selectedProfile, setSelectedProfile] = useState("");
  const {
    data: status,
    isPending,
    error,
  } = useQuery({ ...linuxio.power.get_status, refetchInterval: 15000 });

  const powerActionConfig = (message: string) => ({
    success: (nextStatus: PowerStatus) => {
      powerStatusCache.set(nextStatus);
      toast.success(message);
    },
    error: "Power action failed",
  });

  const startMutation = useCallMutation(
    linuxio.power.start,
    powerActionConfig("TuneD started"),
  );

  const setProfileMutation = useCallMutation(
    linuxio.power.set_profile,
    powerActionConfig("Power profile applied"),
  );

  const disableMutation = useCallMutation(
    linuxio.power.disable,
    powerActionConfig("TuneD tunings disabled"),
  );

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

  const powerHeader = (
    <div className="power-settings__header">
      <AppTypography fontWeight={600} variant="body1">
        Power
      </AppTypography>
      <AppTypography color="text.secondary" variant="caption">
        Manage TuneD status and power profiles.
      </AppTypography>
    </div>
  );

  if (isPending) {
    return (
      <div aria-busy className="power-settings">
        {powerHeader}
        <div className="power-settings__loading">
          <ComponentLoader />
        </div>
      </div>
    );
  }

  if (error || !status) {
    return (
      <div className="power-settings">
        {powerHeader}
        <AppAlert severity="error">
          <AppAlertTitle>Power status unavailable</AppAlertTitle>
          {error?.message || "LinuxIO could not read power management status."}
        </AppAlert>
      </div>
    );
  }

  const renderProfileOption = (value: string) => {
    const profile = status.profiles.find((p) => p.name === value);
    return (
      <>
        <span>{value}</span>
        {profile?.active && <PowerBadge label="Active" tone="success" />}
        {profile?.recommended && <PowerBadge label="Recommended" tone="info" />}
      </>
    );
  };

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
    <div aria-busy={busy} className="power-settings">
      {powerHeader}

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

      <FrostedCard className="power-settings__card">
        <div className="power-settings__card-header">
          <div className="power-settings__card-title">
            <div className="power-settings__card-icon">
              <Icon height={22} icon="mdi:tune-variant" width={22} />
            </div>
            <div className="power-settings__title-block">
              <AppTypography component="h3" fontWeight={600} variant="body2">
                TuneD Status
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                System power tuning service.
              </AppTypography>
            </div>
          </div>
          <div className="power-settings__card-actions">
            <StatusBadge status={status} />
            <AppActionIconButton
              ariaLabel={
                startMutation.isPending
                  ? "Starting TuneD"
                  : disableMutation.isPending
                    ? "Disabling TuneD"
                    : status.tuned_active
                      ? "Turn Off TuneD"
                      : "Start TuneD"
              }
              color={
                status.tuned_active
                  ? "var(--app-palette-success-main)"
                  : status.tuned_available && status.tuned_startable
                    ? "var(--app-palette-error-main)"
                    : "var(--app-palette-text-disabled)"
              }
              disabled={
                busy ||
                !status.tuned_available ||
                (!status.tuned_active && !status.tuned_startable)
              }
              icon="mdi:power"
              iconSize={22}
              label={
                startMutation.isPending
                  ? "Starting TuneD"
                  : disableMutation.isPending
                    ? "Disabling TuneD"
                    : status.tuned_active
                      ? "Turn Off"
                      : status.tuned_startable
                        ? "Start TuneD"
                        : "TuneD cannot be started automatically"
              }
              loading={startMutation.isPending || disableMutation.isPending}
              onClick={() =>
                status.tuned_active
                  ? disableMutation.mutate()
                  : startMutation.mutate()
              }
            />
          </div>
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
      </FrostedCard>

      <FrostedCard className="power-settings__card">
        <div className="power-settings__card-header">
          <div className="power-settings__card-title">
            <div className="power-settings__card-icon">
              <Icon height={22} icon="mdi:speedometer" width={22} />
            </div>
            <div className="power-settings__title-block">
              <AppTypography component="h3" fontWeight={600} variant="body2">
                Profile
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Available TuneD profiles.
              </AppTypography>
            </div>
          </div>
        </div>
        <div className="power-settings__profile-control">
          <span className="power-settings__label power-settings__label--span">
            Available profiles
          </span>
          <AppSelect
            disabled={busy || status.profiles.length === 0}
            fullWidth
            onChange={(event) => setSelectedProfile(event.target.value)}
            renderOption={renderProfileOption}
            renderValue={renderProfileOption}
            size="small"
            value={resolvedProfile}
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
            disabled={!canApplyProfile}
            onClick={() =>
              setProfileMutation.mutate({ profile: resolvedProfile })
            }
            size="small"
            startIcon={<Icon height={18} icon="mdi:check" width={18} />}
            variant={selectedIsActive ? "text" : "contained"}
          >
            {profileActionLabel}
          </AppButton>
        </div>

        {selectedProfileDetails?.description ? (
          <p className="power-settings__muted">
            {selectedProfileDetails.description}
          </p>
        ) : !selectedProfileDetails ? (
          <p className="power-settings__muted">No TuneD profiles reported.</p>
        ) : null}
      </FrostedCard>
    </div>
  );
};

export default PowerSettingsSection;
