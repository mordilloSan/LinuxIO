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
  type CapabilityErrorKey,
  type CapabilityKey,
  type CapabilityValueKey,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
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
  const mountedRef = useRef(true);

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

        return {
          ...item,
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
              key={row.state}
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
