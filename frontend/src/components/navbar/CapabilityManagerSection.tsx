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

import type { CapabilitiesResponse } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppChip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import {
  type CapabilityKey,
  type CapabilityStatus,
  getCapabilityReason,
  getCapabilityStatus,
} from "@/hooks/useCapabilities";

type CapabilityValueKey =
  | "docker_available"
  | "indexer_available"
  | "lm_sensors_available"
  | "smartmontools_available"
  | "packagekit_available"
  | "nfs_client_available"
  | "nfs_server_available"
  | "tuned_available";

type CapabilityErrorKey =
  | "docker_error"
  | "indexer_error"
  | "lm_sensors_error"
  | "smartmontools_error"
  | "packagekit_error"
  | "nfs_client_error"
  | "nfs_server_error"
  | "tuned_error";

interface CapabilityItem {
  authKey: CapabilityKey;
  valueKey: CapabilityValueKey;
  errorKey: CapabilityErrorKey;
  label: string;
  description: string;
  readyText: string;
  dependency: string;
  icon: string;
}

const CAPABILITY_ITEMS: CapabilityItem[] = [
  {
    authKey: "dockerAvailable",
    valueKey: "docker_available",
    errorKey: "docker_error",
    label: "Docker",
    description: "Container dashboard and compose stack controls",
    readyText: "Docker is reachable.",
    dependency: "docker",
    icon: "mdi:docker",
  },
  {
    authKey: "indexerAvailable",
    valueKey: "indexer_available",
    errorKey: "indexer_error",
    label: "Indexer",
    description: "Search, folder sizes, and Docker stack indexing",
    readyText: "Indexer service is reachable.",
    dependency: "linuxio indexer",
    icon: "mdi:magnify-scan",
  },
  {
    authKey: "lmSensorsAvailable",
    valueKey: "lm_sensors_available",
    errorKey: "lm_sensors_error",
    label: "lm-sensors",
    description: "Hardware sensors and thermal readings",
    readyText: "sensors command is available.",
    dependency: "sensors",
    icon: "mdi:thermometer-lines",
  },
  {
    authKey: "smartmontoolsAvailable",
    valueKey: "smartmontools_available",
    errorKey: "smartmontools_error",
    label: "smartmontools",
    description: "Drive SMART attributes and self-tests",
    readyText: "smartctl command is available.",
    dependency: "smartctl",
    icon: "mdi:harddisk",
  },
  {
    authKey: "packageKitAvailable",
    valueKey: "packagekit_available",
    errorKey: "packagekit_error",
    label: "PackageKit",
    description: "Package update checks and package operations",
    readyText: "PackageKit D-Bus service is reachable.",
    dependency: "PackageKit",
    icon: "mdi:package-variant-closed",
  },
  {
    authKey: "nfsClientAvailable",
    valueKey: "nfs_client_available",
    errorKey: "nfs_client_error",
    label: "NFS client",
    description: "Mount external NFS exports",
    readyText: "NFS client utilities are available.",
    dependency: "nfs utilities",
    icon: "mdi:folder-network-outline",
  },
  {
    authKey: "nfsServerAvailable",
    valueKey: "nfs_server_available",
    errorKey: "nfs_server_error",
    label: "NFS server",
    description: "Create and manage exported NFS shares",
    readyText: "NFS server utilities are available.",
    dependency: "exportfs",
    icon: "mdi:server-network",
  },
  {
    authKey: "tunedAvailable",
    valueKey: "tuned_available",
    errorKey: "tuned_error",
    label: "TuneD",
    description: "Power profile management",
    readyText: "TuneD D-Bus service is reachable.",
    dependency: "TuneD",
    icon: "mdi:lightning-bolt-outline",
  },
];

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
  const {
    dockerAvailable,
    indexerAvailable,
    lmSensorsAvailable,
    smartmontoolsAvailable,
    packageKitAvailable,
    nfsClientAvailable,
    nfsServerAvailable,
    tunedAvailable,
    refreshCapabilities,
  } = useAuth();

  const [latest, setLatest] = useState<CapabilitiesResponse | null>(null);
  const [lastChecked, setLastChecked] = useState<Date | null>(null);
  const [isRefreshing, setIsRefreshing] = useState(true);
  const [errorText, setErrorText] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const rows = useMemo(
    () =>
      CAPABILITY_ITEMS.map((item) => {
        const authValue = {
          dockerAvailable,
          indexerAvailable,
          lmSensorsAvailable,
          smartmontoolsAvailable,
          packageKitAvailable,
          nfsClientAvailable,
          nfsServerAvailable,
          tunedAvailable,
        }[item.authKey];
        const value = latest?.[item.valueKey] ?? authValue;
        const status = getCapabilityStatus(value);
        const detail =
          latest?.[item.errorKey] ||
          (status === "available"
            ? item.readyText
            : getCapabilityReason(item.authKey, status));

        return {
          ...item,
          status,
          detail,
        };
      }),
    [
      dockerAvailable,
      indexerAvailable,
      lmSensorsAvailable,
      smartmontoolsAvailable,
      packageKitAvailable,
      nfsClientAvailable,
      nfsServerAvailable,
      tunedAvailable,
      latest,
    ],
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
    <div className="capability-manager" aria-busy={isRefreshing}>
      <div className="capability-manager__header">
        <div>
          <AppTypography variant="body1" fontWeight={600}>
            Capability Manager
          </AppTypography>
          <AppTypography variant="caption" color="text.secondary">
            Last check: {formatLastChecked(lastChecked)}
          </AppTypography>
        </div>
        <AppTooltip title={isRefreshing ? "Checking" : "Refresh"}>
          <AppIconButton
            size="small"
            color="default"
            disabled={isRefreshing}
            onClick={() => void handleRefresh()}
            aria-label={
              isRefreshing ? "Checking capabilities" : "Refresh capabilities"
            }
          >
            <Icon
              icon={isRefreshing ? "mdi:loading" : "mdi:refresh"}
              width={18}
              height={18}
              className={isRefreshing ? "capability-manager__spin" : undefined}
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
          return (
            <FrostedCard
              key={row.authKey}
              className="capability-manager__row"
              hoverLift
            >
              <div className="capability-manager__icon">
                <Icon icon={row.icon} width={22} height={22} />
              </div>
              <div className="capability-manager__body">
                <div className="capability-manager__row-header">
                  <div className="capability-manager__title-block">
                    <AppTypography
                      variant="body2"
                      fontWeight={600}
                      component="h3"
                    >
                      {row.label}
                    </AppTypography>
                    <AppTypography variant="caption" color="text.secondary">
                      {row.description}
                    </AppTypography>
                  </div>
                  <AppChip
                    size="small"
                    variant="soft"
                    color={status.color}
                    label={status.label}
                  />
                </div>
                <div className="capability-manager__detail">
                  <span className="capability-manager__dependency">
                    {row.dependency}
                  </span>
                  <span>{row.detail}</span>
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
