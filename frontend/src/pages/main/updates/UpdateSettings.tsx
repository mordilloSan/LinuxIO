import {
  Box,
  Typography,
  Switch,
  FormControlLabel,
  Select,
  MenuItem,
  SelectChangeEvent,
  TextField,
  Button,
  Chip,
  Stack,
} from "@mui/material";
import React, { useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api/linuxio";
import ComponentLoader from "@/components/loaders/ComponentLoader";

type Frequency = "hourly" | "daily" | "weekly";
type Scope = "security" | "updates" | "all";
type RebootPolicy = "never" | "if_needed" | "always" | "schedule";

const updatesToastMeta = { meta: { href: "/updates", label: "Open updates" } };

interface AutoUpdateOptions {
  enabled: boolean;
  frequency: Frequency;
  scope: Scope;
  download_only: boolean;
  reboot_policy: RebootPolicy;
  exclude_packages: string[];
}

interface AutoUpdateState {
  backend: string;
  options: AutoUpdateOptions;
  notes?: string[];
}

const normalizeState = (s: AutoUpdateState): AutoUpdateState => ({
  ...s,
  options: {
    ...s.options,
    exclude_packages: Array.isArray(s.options.exclude_packages)
      ? s.options.exclude_packages
      : [],
  },
});

const UpdateSettings: React.FC = () => {
  // -------- Load auto update settings --------
  const {
    data: rawServerState,
    isLoading: loading,
    refetch,
  } = linuxio.useCall<AutoUpdateState>("dbus", "GetAutoUpdates");

  const serverState = useMemo(
    () => (rawServerState ? normalizeState(rawServerState) : null),
    [rawServerState],
  );

  // Local draft state - null means "use server value"
  const [draftOverrides, setDraftOverrides] =
    useState<Partial<AutoUpdateOptions> | null>(null);
  const [excludeInputOverride, setExcludeInputOverride] = useState<
    string | null
  >(null);

  // Derived current values (draft overrides server)
  const currentOptions: AutoUpdateOptions | null = useMemo(() => {
    if (!serverState) return null;
    return {
      ...serverState.options,
      ...draftOverrides,
    };
  }, [serverState, draftOverrides]);

  const currentExcludeInput = useMemo(() => {
    if (excludeInputOverride !== null) return excludeInputOverride;
    return serverState?.options.exclude_packages.join(", ") ?? "";
  }, [serverState, excludeInputOverride]);

  // -------- Mutations --------
  const setAutoUpdatesMutation = linuxio.useMutate<
    AutoUpdateState,
    AutoUpdateOptions
  >("dbus", "SetAutoUpdates");

  const applyOfflineMutation = linuxio.useMutate<{
    status?: string;
    error?: string;
  }>("dbus", "ApplyOfflineUpdates");

  const saving =
    setAutoUpdatesMutation.isPending || applyOfflineMutation.isPending;

  // dirty check for enabling Save/Cancel
  const dirty = useMemo(() => {
    if (!serverState || !currentOptions) return false;
    // compare with excludeInput normalized into list, so the button reflects pending text edits
    const draftWithExcludes: AutoUpdateOptions = {
      ...currentOptions,
      exclude_packages: currentExcludeInput
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
    };
    return (
      JSON.stringify(serverState.options) !== JSON.stringify(draftWithExcludes)
    );
  }, [serverState, currentOptions, currentExcludeInput]);

  // -------- Save (explicit) --------
  const save = () => {
    if (!currentOptions) return;

    const payload: AutoUpdateOptions = {
      ...currentOptions,
      exclude_packages: currentExcludeInput
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
    };

    setAutoUpdatesMutation.mutate(payload, {
      onSuccess: () => {
        // Clear overrides - server now has the saved values
        setDraftOverrides(null);
        setExcludeInputOverride(null);
        // Refetch to update UI with server state
        refetch();
        toast.success("Automatic Updates Settings saved", updatesToastMeta);
      },
    });
  };

  // -------- Apply at next reboot --------
  const handleApplyOffline = () => {
    applyOfflineMutation.mutate(undefined, {
      onSuccess: (result) => {
        if (result?.status && result.status !== "ok") {
          const errMsg = result.error || "Failed to schedule offline update";
          // Show friendly info message for "no updates" case
          if (
            errMsg.includes("no updates available") ||
            errMsg.includes("Prepared update not found")
          ) {
            toast.info("No updates available to schedule", updatesToastMeta);
          }
          // Other errors handled by global QueryClient config
          return;
        }
        toast.success(
          "Offline update scheduled for next reboot",
          updatesToastMeta,
        );
      },
      onError: (err) => {
        const errMsg = err?.message || String(err);
        // Show friendly info message for "no updates" case
        if (
          errMsg.includes("no updates available") ||
          errMsg.includes("Prepared update not found")
        ) {
          toast.info("No updates available to schedule", updatesToastMeta);
        }
        // Other errors handled by global QueryClient config
      },
    });
  };

  if (loading || !serverState || !currentOptions) {
    return <ComponentLoader />;
  }

  return (
    <Box sx={{ p: 3, display: "grid", gap: 2 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Typography variant="h6" sx={{ flexGrow: 1 }}>
          Automatic Updates
        </Typography>
        <Chip size="small" label={serverState.backend} variant="outlined" />
      </Box>

      <FormControlLabel
        control={
          <Switch
            checked={currentOptions.enabled}
            onChange={(e) =>
              setDraftOverrides({
                ...(draftOverrides ?? {}),
                enabled: e.target.checked,
              })
            }
            disabled={saving}
          />
        }
        label="Enable automatic updates"
      />

      <Stack
        direction="row"
        spacing={3}
        sx={{ alignItems: "center", flexWrap: "wrap" }}
      >
        <Box>
          <Typography variant="subtitle2" gutterBottom>
            Frequency
          </Typography>
          <Select
            size="small"
            value={currentOptions.frequency}
            onChange={(e: SelectChangeEvent<Frequency>) =>
              setDraftOverrides({
                ...(draftOverrides ?? {}),
                frequency: e.target.value as Frequency,
              })
            }
            disabled={saving}
          >
            <MenuItem value="hourly">Hourly</MenuItem>
            <MenuItem value="daily">Daily</MenuItem>
            <MenuItem value="weekly">Weekly</MenuItem>
          </Select>
        </Box>

        <Box>
          <Typography variant="subtitle2" gutterBottom>
            Scope
          </Typography>
          <Select
            size="small"
            value={currentOptions.scope}
            onChange={(e: SelectChangeEvent<Scope>) =>
              setDraftOverrides({
                ...(draftOverrides ?? {}),
                scope: e.target.value as Scope,
              })
            }
            disabled={saving}
          >
            <MenuItem value="security">Security only</MenuItem>
            <MenuItem value="updates">Security + updates</MenuItem>
            <MenuItem value="all">All (incl. extras)</MenuItem>
          </Select>
        </Box>

        <Box>
          <Typography variant="subtitle2" gutterBottom>
            Reboot policy
          </Typography>
          <Select
            size="small"
            value={currentOptions.reboot_policy}
            onChange={(e: SelectChangeEvent<RebootPolicy>) =>
              setDraftOverrides({
                ...(draftOverrides ?? {}),
                reboot_policy: e.target.value as RebootPolicy,
              })
            }
            disabled={saving}
          >
            <MenuItem value="never">Never</MenuItem>
            <MenuItem value="if_needed">If needed</MenuItem>
            <MenuItem value="always">Always</MenuItem>
          </Select>
        </Box>

        <FormControlLabel
          control={
            <Switch
              checked={currentOptions.download_only}
              onChange={(e) =>
                setDraftOverrides({
                  ...(draftOverrides ?? {}),
                  download_only: e.target.checked,
                })
              }
              disabled={saving}
            />
          }
          label="Download only (no auto-install)"
        />
      </Stack>

      <Box>
        <Typography variant="subtitle2" gutterBottom>
          Exclude packages (comma-separated)
        </Typography>
        <Stack direction="row" spacing={1} alignItems="center">
          <TextField
            size="small"
            placeholder="e.g. linux-headers-*, docker-ce"
            value={currentExcludeInput}
            onChange={(e) => setExcludeInputOverride(e.target.value)}
            disabled={saving}
            sx={{ minWidth: 420, maxWidth: 600 }}
          />
        </Stack>
      </Box>

      <Box sx={{ display: "flex", gap: 1, alignItems: "center", mt: 1 }}>
        <Button variant="contained" onClick={save} disabled={saving || !dirty}>
          Save
        </Button>
        <Button
          variant="text"
          onClick={() => {
            // Revert to server values by clearing overrides
            setDraftOverrides(null);
            setExcludeInputOverride(null);
          }}
          disabled={saving || !dirty}
        >
          Cancel
        </Button>
        <Box sx={{ flexGrow: 1 }} />
        <Button
          variant="contained"
          onClick={handleApplyOffline}
          disabled={saving}
        >
          Apply at next reboot (offline)
        </Button>
        {serverState.notes?.length ? (
          <Typography variant="body2" color="text.secondary">
            {serverState.notes.join(" • ")}
          </Typography>
        ) : null}
      </Box>
    </Box>
  );
};

export default UpdateSettings;
