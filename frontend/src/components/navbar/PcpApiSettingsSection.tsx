import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio, type PcpApiConfig, type PcpApiExposurePolicy } from "@/api";
import PageLoader from "@/components/loaders/PageLoader";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

const CATEGORY_LABELS: Record<string, string> = {
  summary: "Summary",
  cpu: "CPU",
  memory: "Memory",
  network: "Network",
  disk: "Disk",
  filesystems: "Filesystems",
  thermal: "Thermal",
  system: "System",
};

const ENDPOINT_LABELS: Record<string, string> = {
  "/api/v1/summary": "Summary",
  "/api/v1/cpu": "CPU",
  "/api/v1/memory": "Memory",
  "/api/v1/network": "Network",
  "/api/v1/disk": "Disk",
  "/api/v1/filesystems": "Filesystems",
  "/api/v1/thermal": "Thermal",
  "/api/v1/system": "System",
};

type EndpointOverrideValue = "" | PcpApiExposurePolicy;

function cloneConfig(config: PcpApiConfig): PcpApiConfig {
  return {
    ...config,
    auth: { ...config.auth },
    exposure: {
      categories: { ...config.exposure.categories },
      endpoints: { ...config.exposure.endpoints },
    },
  };
}

function defaultDraft(config: PcpApiConfig): PcpApiConfig {
  const draft = cloneConfig(config);
  for (const category of Object.keys(CATEGORY_LABELS)) {
    if (!draft.exposure.categories[category]) {
      draft.exposure.categories[category] = "private";
    }
  }
  return draft;
}

const PcpApiSettingsSection = () => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const {
    data: config,
    isPending: configLoading,
    refetch: refetchConfig,
  } = linuxio.pcp_api.get_config.useQuery();
  const {
    data: status,
    isPending: statusLoading,
    refetch: refetchStatus,
  } = linuxio.pcp_api.get_status.useQuery({ refetchInterval: 5000 });

  const [draft, setDraft] = useState<PcpApiConfig | null>(null);
  const [token, setToken] = useState<string>("");
  const [tokenPath, setTokenPath] = useState<string>("");
  const [showToken, setShowToken] = useState(false);

  const sourceConfig = useMemo(
    () => (config ? defaultDraft(config) : null),
    [config],
  );
  const effectiveDraft = draft ?? sourceConfig;

  const updateDraft = (updater: (current: PcpApiConfig) => PcpApiConfig) => {
    setDraft((current) => {
      const base = current ?? sourceConfig;
      if (!base) return current;
      return updater(base);
    });
  };

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: linuxio.pcp_api.get_config.queryKey(),
      }),
      queryClient.invalidateQueries({
        queryKey: linuxio.pcp_api.get_status.queryKey(),
      }),
    ]);
    await Promise.all([refetchConfig(), refetchStatus()]);
  };

  const { mutate: saveConfig, isPending: savePending } =
    linuxio.pcp_api.set_config.useMutation({
      onSuccess: async (nextConfig) => {
        setDraft(defaultDraft(nextConfig));
        await refresh();
        toast.success("PCP API settings saved");
      },
      onError: (error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to save PCP API settings"),
        );
      },
    });

  const { mutate: reloadService, isPending: reloadPending } =
    linuxio.pcp_api.reload_service.useMutation({
      onSuccess: async () => {
        await refresh();
        toast.success("PCP API reloaded");
      },
      onError: (error) => {
        toast.error(getMutationErrorMessage(error, "Failed to reload PCP API"));
      },
    });

  const { mutate: restartService, isPending: restartPending } =
    linuxio.pcp_api.restart_service.useMutation({
      onSuccess: async () => {
        await refresh();
        toast.success("PCP API restarted");
      },
      onError: (error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to restart PCP API"),
        );
      },
    });

  const { mutate: rotateToken, isPending: rotatePending } =
    linuxio.pcp_api.rotate_token.useMutation({
      onSuccess: async (response) => {
        setToken(response.token);
        setTokenPath(response.path);
        setShowToken(true);
        await refresh();
        toast.success("PCP API token rotated");
      },
      onError: (error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to rotate PCP API token"),
        );
      },
    });

  const loading = configLoading || statusLoading || !effectiveDraft;
  const busy = savePending || reloadPending || restartPending || rotatePending;

  const endpointOverrides = useMemo(() => {
    const values: Record<string, EndpointOverrideValue> = {};
    for (const endpoint of Object.keys(ENDPOINT_LABELS)) {
      values[endpoint] = effectiveDraft?.exposure.endpoints[endpoint] ?? "";
    }
    return values;
  }, [effectiveDraft]);

  const handleSave = () => {
    if (!effectiveDraft) return;
    saveConfig([effectiveDraft]);
  };

  const handleRevealToken = async () => {
    try {
      const response = await linuxio.pcp_api.get_token.call();
      setToken(response.token);
      setTokenPath(response.path);
      setShowToken(true);
    } catch (error) {
      toast.error(
        getMutationErrorMessage(error, "Failed to read PCP API token"),
      );
    }
  };

  const handleCopyToken = async () => {
    if (!token) return;
    try {
      await navigator.clipboard.writeText(token);
      toast.success("PCP API token copied");
    } catch {
      toast.error("Failed to copy PCP API token");
    }
  };

  const setCategoryPolicy = (
    category: string,
    policy: PcpApiExposurePolicy,
  ) => {
    updateDraft((current) => ({
      ...current,
      exposure: {
        ...current.exposure,
        categories: {
          ...current.exposure.categories,
          [category]: policy,
        },
      },
    }));
  };

  const setEndpointOverride = (
    endpoint: string,
    policy: EndpointOverrideValue,
  ) => {
    updateDraft((current) => {
      const endpoints = { ...current.exposure.endpoints };
      if (!policy) {
        delete endpoints[endpoint];
      } else {
        endpoints[endpoint] = policy;
      }

      return {
        ...current,
        exposure: {
          ...current.exposure,
          endpoints,
        },
      };
    });
  };

  if (loading || !effectiveDraft) {
    return <PageLoader />;
  }

  const statusAlertSeverity = !status?.healthy
    ? "warning"
    : status.active_state === "active"
      ? "success"
      : "info";

  return (
    <div
      style={{
        display: "grid",
        gap: theme.spacing(2),
        paddingTop: theme.spacing(1),
      }}
    >
      <AppAlert severity={statusAlertSeverity}>
        <div>
          <strong>Service status:</strong> {status?.active_state ?? "unknown"}
          {status?.version ? ` · ${status.version}` : ""}
          {status?.healthy ? " · healthy" : ""}
          {status?.health_error ? ` · ${status.health_error}` : ""}
        </div>
      </AppAlert>

      <AppAlert severity="info">
        Listen address changes are written immediately, but the running service
        needs a restart before the new socket is used.
      </AppAlert>

      <div
        style={{
          display: "grid",
          gap: theme.spacing(1.5),
        }}
      >
        <AppTypography variant="body1" fontWeight={600}>
          Runtime
        </AppTypography>

        <AppFormControlLabel
          control={
            <AppSwitch
              checked={effectiveDraft.enabled}
              onChange={(event) =>
                updateDraft((current) => ({
                  ...current,
                  enabled: event.target.checked,
                }))
              }
              disabled={busy}
            />
          }
          label="Enable PCP API service"
        />

        <AppFormControlLabel
          control={
            <AppSwitch
              checked={effectiveDraft.auth.enabled}
              onChange={(event) =>
                updateDraft((current) => ({
                  ...current,
                  auth: {
                    ...current.auth,
                    enabled: event.target.checked,
                  },
                }))
              }
              disabled={busy}
            />
          }
          label="Require bearer token for private endpoints"
        />

        <AppTextField
          label="Listen address"
          value={effectiveDraft.listen_address}
          onChange={(event) =>
            updateDraft((current) => ({
              ...current,
              listen_address: event.target.value,
            }))
          }
          fullWidth
          disabled={busy}
        />

        <AppTextField
          label="Token file"
          value={effectiveDraft.auth.token_file}
          fullWidth
          disabled
        />
      </div>

      <div
        style={{
          display: "grid",
          gap: theme.spacing(1.25),
        }}
      >
        <AppTypography variant="body1" fontWeight={600}>
          Category visibility
        </AppTypography>

        {Object.entries(CATEGORY_LABELS).map(([category, label]) => (
          <div
            key={category}
            style={{
              display: "grid",
              gridTemplateColumns: "minmax(160px, 1fr) minmax(150px, 220px)",
              gap: theme.spacing(1),
              alignItems: "center",
            }}
          >
            <AppTypography variant="body2">{label}</AppTypography>
            <AppSelect
              size="small"
              value={effectiveDraft.exposure.categories[category] ?? "private"}
              onChange={(event) =>
                setCategoryPolicy(
                  category,
                  event.target.value as PcpApiExposurePolicy,
                )
              }
              disabled={busy}
            >
              <option value="public">Public</option>
              <option value="private">Private</option>
            </AppSelect>
          </div>
        ))}
      </div>

      <div
        style={{
          display: "grid",
          gap: theme.spacing(1.25),
        }}
      >
        <AppTypography variant="body1" fontWeight={600}>
          Endpoint overrides
        </AppTypography>

        {Object.entries(ENDPOINT_LABELS).map(([endpoint, label]) => (
          <div
            key={endpoint}
            style={{
              display: "grid",
              gridTemplateColumns: "minmax(160px, 1fr) minmax(150px, 220px)",
              gap: theme.spacing(1),
              alignItems: "center",
            }}
          >
            <div>
              <AppTypography variant="body2">{label}</AppTypography>
              <AppTypography variant="caption" color="text.secondary">
                {endpoint}
              </AppTypography>
            </div>
            <AppSelect
              size="small"
              value={endpointOverrides[endpoint]}
              onChange={(event) =>
                setEndpointOverride(
                  endpoint,
                  event.target.value as EndpointOverrideValue,
                )
              }
              disabled={busy}
            >
              <option value="">Category default</option>
              <option value="public">Public</option>
              <option value="private">Private</option>
            </AppSelect>
          </div>
        ))}
      </div>

      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: theme.spacing(1),
        }}
      >
        <AppButton variant="contained" onClick={handleSave} disabled={busy}>
          Save
        </AppButton>
        <AppButton
          variant="outlined"
          onClick={() => reloadService([])}
          disabled={busy}
        >
          Reload
        </AppButton>
        <AppButton
          variant="outlined"
          onClick={() => restartService([])}
          disabled={busy}
        >
          Restart
        </AppButton>
        <AppButton
          variant="outlined"
          onClick={handleRevealToken}
          disabled={busy}
        >
          Reveal token
        </AppButton>
        <AppButton
          variant="outlined"
          onClick={() => rotateToken([])}
          disabled={busy}
        >
          Rotate token
        </AppButton>
      </div>

      {showToken && (
        <div
          style={{
            display: "grid",
            gap: theme.spacing(1),
          }}
        >
          <AppTextField
            label={tokenPath ? `Bearer token · ${tokenPath}` : "Bearer token"}
            value={token}
            fullWidth
            disabled
          />
          <div
            style={{
              display: "flex",
              gap: theme.spacing(1),
            }}
          >
            <AppButton variant="outlined" onClick={handleCopyToken}>
              Copy token
            </AppButton>
            <AppButton variant="text" onClick={() => setShowToken(false)}>
              Hide token
            </AppButton>
          </div>
        </div>
      )}
    </div>
  );
};

export default PcpApiSettingsSection;
