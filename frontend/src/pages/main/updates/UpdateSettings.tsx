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
import React, { useEffect, useMemo, useState } from "react";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import { streamApi } from "@/utils/streamApi";
import { toast } from "sonner";

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
  const [serverState, setServerState] = useState<AutoUpdateState | null>(null);
  const [draft, setDraft] = useState<AutoUpdateOptions | null>(null);
  const [excludeInput, setExcludeInput] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  // -------- Load on mount --------
  useEffect(() => {
    let mounted = true;
    streamApi.get<AutoUpdateState>("dbus", "GetAutoUpdates")
      .then((payload) => {
        if (!mounted) return;
        if (!payload) {
          throw new Error("Empty auto update response");
        }
        const norm = normalizeState(payload);
        setServerState(norm);
        setDraft(norm.options);
        setExcludeInput(norm.options.exclude_packages.join(", "));
      })
      .catch((err) => {
        console.error("Failed to load auto update settings", err);
        toast.error("Failed to load auto update settings", updatesToastMeta);
      })
      .finally(() => setLoading(false));
    return () => {
      mounted = false;
    };
  }, []);

  // dirty check for enabling Save/Cancel
  const dirty = useMemo(() => {
    if (!serverState || !draft) return false;
    // compare with excludeInput normalized into list, so the button reflects pending text edits
    const draftWithExcludes: AutoUpdateOptions = {
      ...draft,
      exclude_packages: excludeInput
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
    };
    return (
      JSON.stringify(serverState.options) !== JSON.stringify(draftWithExcludes)
    );
  }, [serverState, draft, excludeInput]);

  // -------- Save (explicit) --------
  const save = async () => {
    if (!draft) return;
    setSaving(true);
    try {
      const payload: AutoUpdateOptions = {
        ...draft,
        exclude_packages: excludeInput
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
      };
      // SetAutoUpdates expects a single JSON arg
      const result = await streamApi.get<AutoUpdateState>(
        "dbus",
        "SetAutoUpdates",
        [JSON.stringify(payload)],
      );
      if (!result) {
        throw new Error("Empty auto update response");
      }
      const norm = normalizeState(result);
      setServerState(norm);
      setDraft(norm.options);
      setExcludeInput(norm.options.exclude_packages.join(", "));
      toast.success("Automatic Updates Settings saved", updatesToastMeta);
    } catch (err) {
      console.error("Failed to save auto update settings", err);
      toast.error("Failed to save settings", updatesToastMeta);
    } finally {
      setSaving(false);
    }
  };

  // -------- Apply at next reboot --------
  const handleApplyOffline = async () => {
    try {
      const result = await streamApi.get<{ status?: string; error?: string }>(
        "dbus",
        "ApplyOfflineUpdates",
      );
      if (result?.status && result.status !== "ok") {
        throw new Error(result.error || "Failed to schedule offline update");
      }
      toast.success(
        "Offline update scheduled for next reboot",
        updatesToastMeta,
      );
    } catch (err: any) {
      const errMsg = err?.message || String(err);
      console.error("Failed to schedule offline update", err);

      // Show friendly messages for common cases
      if (errMsg.includes("no updates available") || errMsg.includes("Prepared update not found")) {
        toast.info("No updates available to schedule", updatesToastMeta);
      } else {
        toast.error("Failed to schedule offline update", updatesToastMeta);
      }
    }
  };

  if (loading || !serverState || !draft) {
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
            checked={draft.enabled}
            onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
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
            value={draft.frequency}
            onChange={(e: SelectChangeEvent<Frequency>) =>
              setDraft({ ...draft, frequency: e.target.value as Frequency })
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
            value={draft.scope}
            onChange={(e: SelectChangeEvent<Scope>) =>
              setDraft({ ...draft, scope: e.target.value as Scope })
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
            value={draft.reboot_policy}
            onChange={(e: SelectChangeEvent<RebootPolicy>) =>
              setDraft({
                ...draft,
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
              checked={draft.download_only}
              onChange={(e) =>
                setDraft({ ...draft, download_only: e.target.checked })
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
            value={excludeInput}
            onChange={(e) => setExcludeInput(e.target.value)}
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
            // Revert draft to server values
            setDraft(serverState.options);
            setExcludeInput(serverState.options.exclude_packages.join(", "));
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
