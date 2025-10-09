import {
  Box,
  Typography,
  Switch,
  FormControlLabel,
  Select,
  MenuItem,
  CircularProgress,
  SelectChangeEvent,
} from "@mui/material";
import React, { useState, useEffect } from "react";

import axios from "@/utils/axios";

type Frequency = "daily" | "weekly" | "monthly";

interface Settings {
  enabled: boolean;
  frequency: Frequency;
  lastRun: string | null;
}

const UpdateSettings: React.FC = () => {
  const [settings, setSettings] = useState<Settings>({
    enabled: false,
    frequency: "daily",
    lastRun: null,
  });

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    axios
      .get("/updates/settings")
      .then((res) => {
        setSettings(res.data);
      })
      .catch((err) => {
        console.error("Failed to load update settings", err);
      })
      .finally(() => setLoading(false));
  }, []);

  const handleToggle = async (enabled: boolean) => {
    setSettings((prev) => ({ ...prev, enabled }));
    await saveSettings({ ...settings, enabled });
  };

  const handleFrequencyChange = async (event: SelectChangeEvent<Frequency>) => {
    const frequency = event.target.value as Frequency;
    setSettings((prev) => ({ ...prev, frequency }));
    await saveSettings({ ...settings, frequency });
  };

  const saveSettings = async (newSettings: Partial<Settings>) => {
    setSaving(true);
    try {
      await axios.post("/updates/settings", {
        enabled: newSettings.enabled,
        frequency: newSettings.frequency,
      });
    } catch (err) {
      console.error("Failed to save update settings", err);
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <Box sx={{ p: 3 }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h6" gutterBottom>
        Automatic Updates Settings
      </Typography>

      <FormControlLabel
        control={
          <Switch
            checked={settings.enabled}
            onChange={(e) => handleToggle(e.target.checked)}
            disabled={saving}
          />
        }
        label="Enable automatic updates"
      />

      <Box sx={{ mt: 2 }}>
        <Typography variant="subtitle1" gutterBottom>
          Update frequency
        </Typography>
        <Select
          size="small"
          value={settings.frequency}
          onChange={handleFrequencyChange}
          disabled={saving}
        >
          <MenuItem value="daily">Daily</MenuItem>
          <MenuItem value="weekly">Weekly</MenuItem>
          <MenuItem value="monthly">Monthly</MenuItem>
        </Select>
      </Box>

      {settings.lastRun && (
        <Box sx={{ mt: 3 }}>
          <Typography variant="body2" color="text.secondary">
            Last run: {new Date(settings.lastRun).toLocaleString()}
          </Typography>
        </Box>
      )}
    </Box>
  );
};

export default UpdateSettings;