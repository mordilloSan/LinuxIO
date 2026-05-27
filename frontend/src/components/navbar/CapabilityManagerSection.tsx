import { Icon } from "@iconify/react";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import "./capability-manager-section.css";

import {
  CAPABILITIES,
  type CapabilitiesResponse,
  type CapabilityDef,
  type CapabilityErrorKey,
  type CapabilityKey,
  type CapabilityValueKey,
  type InstallCapabilityResult,
  linuxio,
  openJobAttachStream,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import {
  type CapabilityStatus,
  getCapabilityReason,
  getCapabilityStatus,
} from "@/hooks/useCapabilities";
import { useStreamResult } from "@/hooks/useStreamResult";

interface InstallCapabilityProgress {
  message: string;
  stage: string;
}

const STATUS_DETAILS: Record<
  CapabilityStatus,
  { label: string; color: "default" | "success" | "warning" }
> = {
  available: { label: "Available", color: "success" },
  unavailable: { label: "Unavailable", color: "warning" },
  unknown: { label: "Unknown", color: "default" },
};

const formatLastChecked = (value: Date | null) => {
  if (!value) return "Saved sign-in snapshot";
  return value.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
};

const CapabilityManagerSection: React.FC = () => {
  const auth = useAuth();
  const { refreshCapabilities } = auth;

  const [latest, setLatest] = useState<CapabilitiesResponse | null>(null);
  const [lastChecked, setLastChecked] = useState<Date | null>(null);
  const [isRefreshing, setIsRefreshing] = useState(true);
  const [errorText, setErrorText] = useState<string | null>(null);
  const [installingWire, setInstallingWire] = useState<string | null>(null);
  const [installStatus, setInstallStatus] = useState<string | null>(null);
  const mountedRef = useRef(true);
  const { run: runStreamResult } = useStreamResult();

  const packageKitAvailable =
    latest?.packagekit_available ?? auth.packageKitAvailable ?? false;

  const rows = useMemo(
    () =>
      CAPABILITIES.map((item) => {
        const valueKey = `${item.wire}_available` as CapabilityValueKey;
        const errorKey = `${item.wire}_error` as CapabilityErrorKey;
        const authValue = auth[item.state as CapabilityKey];
        const value = latest?.[valueKey] ?? authValue;
        const status = getCapabilityStatus(value);
        const detail =
          latest?.[errorKey] ||
          (status === "available"
            ? item.readyText
            : getCapabilityReason(item.state as CapabilityKey, status));
        const installable = (item as CapabilityDef).installable;

        return {
          ...item,
          installable,
          status,
          detail,
        };
      }),
    [auth, latest],
  );

  const handleRefresh = useCallback(
    async (showSuccessToast = true) => {
      setIsRefreshing(true);
      setErrorText(null);

      try {
        const data = await refreshCapabilities();
        if (!mountedRef.current) return;
        setLatest(data);
        setLastChecked(new Date());
        if (showSuccessToast) {
          toast.success("Capabilities refreshed");
        }
      } catch (error: unknown) {
        const message =
          error instanceof Error
            ? error.message
            : "Failed to refresh capabilities";
        if (!mountedRef.current) return;
        setErrorText(message);
        if (showSuccessToast) {
          toast.error(message);
        }
      } finally {
        if (mountedRef.current) {
          setIsRefreshing(false);
        }
      }
    },
    [refreshCapabilities],
  );

  const handleInstall = useCallback(
    async (wire: string, label: string) => {
      setInstallingWire(wire);
      setInstallStatus("Starting…");
      try {
        const job = await linuxio.system.install_capability.call(wire);
        const result = await runStreamResult<
          InstallCapabilityResult,
          InstallCapabilityProgress
        >({
          open: () => openJobAttachStream(job.id),
          onProgress: (progress) => {
            if (!mountedRef.current) return;
            if (progress?.message) {
              setInstallStatus(progress.message);
            }
          },
        });
        if (!mountedRef.current) return;
        setLatest((previous) => ({
          ...(previous ?? ({} as CapabilitiesResponse)),
          [`${wire}_available`]: result.available,
          [`${wire}_error`]: result.error ?? "",
        }));
        setLastChecked(new Date());
        if (result.available) {
          toast.success(`${label} installed`);
        } else {
          const reason = result.error ? `: ${result.error}` : ".";
          toast.warning(`${label} installed but is still unavailable${reason}`);
        }
      } catch (error: unknown) {
        const message =
          error instanceof Error ? error.message : `Failed to install ${label}`;
        if (mountedRef.current) {
          toast.error(message);
        }
      } finally {
        if (mountedRef.current) {
          setInstallingWire(null);
          setInstallStatus(null);
        }
      }
    },
    [runStreamResult],
  );

  useEffect(
    () => () => {
      mountedRef.current = false;
    },
    [],
  );

  useEffect(() => {
    let cancelled = false;
    refreshCapabilities()
      .then((data) => {
        if (cancelled || !mountedRef.current) return;
        setLatest(data);
        setLastChecked(new Date());
      })
      .catch((error: unknown) => {
        if (cancelled || !mountedRef.current) return;
        setErrorText(
          error instanceof Error
            ? error.message
            : "Failed to refresh capabilities",
        );
      })
      .finally(() => {
        if (!cancelled && mountedRef.current) setIsRefreshing(false);
      });
    return () => {
      cancelled = true;
    };
  }, [refreshCapabilities]);

  return (
    <div aria-busy={isRefreshing} className="capability-manager">
      <div className="capability-manager__header">
        <div>
          <AppTypography fontWeight={600} variant="body1">
            Capability Manager
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Last check: {formatLastChecked(lastChecked)}
          </AppTypography>
        </div>
        <AppTooltip title={isRefreshing ? "Checking" : "Refresh"}>
          <AppIconButton
            aria-label={
              isRefreshing ? "Checking capabilities" : "Refresh capabilities"
            }
            color="default"
            disabled={isRefreshing}
            onClick={() => void handleRefresh()}
            size="small"
          >
            <Icon
              className={isRefreshing ? "capability-manager__spin" : undefined}
              height={18}
              icon={isRefreshing ? "mdi:loading" : "mdi:refresh"}
              width={18}
            />
          </AppIconButton>
        </AppTooltip>
      </div>

      {errorText ? (
        <AppAlert severity="error">
          <AppAlertTitle>Capability check failed</AppAlertTitle>
          {errorText}
        </AppAlert>
      ) : null}

      <div className="capability-manager__list">
        {rows.map((row) => {
          const status = STATUS_DETAILS[row.status];
          const showInstall =
            row.status === "unavailable" && row.installable !== undefined;
          const blockedByPackageKit =
            showInstall &&
            row.installable?.requiresPackageKit === true &&
            !packageKitAvailable;
          const installing = installingWire === row.wire;
          const installDisabled =
            installingWire !== null || blockedByPackageKit;
          const installTooltip = blockedByPackageKit
            ? "Install requires PackageKit, which is itself unavailable. Install PackageKit from a shell first."
            : installing
              ? "Installing…"
              : `Install ${row.label}`;

          return (
            <FrostedCard
              className="capability-manager__row"
              hoverLift
              key={row.state}
            >
              <div className="capability-manager__icon">
                <Icon height={22} icon={row.icon} width={22} />
              </div>
              <div className="capability-manager__body">
                <div className="capability-manager__row-header">
                  <div className="capability-manager__title-block">
                    <AppTypography
                      component="h3"
                      fontWeight={600}
                      variant="body2"
                    >
                      {row.label}
                    </AppTypography>
                    <AppTypography color="text.secondary" variant="caption">
                      {row.description}
                    </AppTypography>
                  </div>
                  <div className="capability-manager__row-actions">
                    {showInstall ? (
                      <AppTooltip title={installTooltip}>
                        <span>
                          <AppButton
                            color="primary"
                            disabled={installDisabled}
                            onClick={() =>
                              void handleInstall(row.wire, row.label)
                            }
                            size="small"
                            startIcon={
                              <Icon
                                className={
                                  installing
                                    ? "capability-manager__spin"
                                    : undefined
                                }
                                height={16}
                                icon={
                                  installing ? "mdi:loading" : "mdi:download"
                                }
                                width={16}
                              />
                            }
                            variant="outlined"
                          >
                            {installing ? "Installing…" : "Install"}
                          </AppButton>
                        </span>
                      </AppTooltip>
                    ) : null}
                    <AppChip
                      color={status.color}
                      label={status.label}
                      size="small"
                      variant="soft"
                    />
                  </div>
                </div>
                <div className="capability-manager__detail">
                  <span className="capability-manager__dependency">
                    {row.dependency}
                  </span>
                  <span>
                    {installing && installStatus ? installStatus : row.detail}
                  </span>
                </div>
              </div>
            </FrostedCard>
          );
        })}
      </div>
    </div>
  );
};

export default CapabilityManagerSection;
