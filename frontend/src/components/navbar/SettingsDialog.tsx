import CloseIcon from "@mui/icons-material/Close";
import {
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  Tab,
  Tabs,
  Typography,
  useTheme,
} from "@mui/material";
import React, { useState } from "react";

import DockerFolderSettingsSection from "./DockerFolderSettingsSection";
import NavbarCustomizer from "./NavbarCustomizer";
import ThemeColorsSection from "./ThemeColorsSection";

import GeneralDialog from "@/components/dialog/GeneralDialog";

type SettingsTab = "general" | "docker";

interface SettingsDialogProps {
  open: boolean;
  onClose: () => void;
}

const SettingsDialog: React.FC<SettingsDialogProps> = ({ open, onClose }) => {
  const theme = useTheme();
  const [activeTab, setActiveTab] = useState<SettingsTab>("general");

  const handleClose = () => {
    setActiveTab("general");
    onClose();
  };

  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="md" fullWidth>
      <DialogTitle
        sx={{
          backgroundColor: theme.palette.background.paper,
          borderBottom: `1px solid ${theme.palette.divider}`,
          py: 1.5,
          px: 2,
        }}
      >
        <Stack direction="row" alignItems="center" sx={{ gap: 1 }}>
          <Typography variant="h6" sx={{ flexGrow: 1 }}>
            Settings
          </Typography>
          <IconButton
            size="small"
            onClick={handleClose}
            aria-label="Close settings"
          >
            <CloseIcon fontSize="small" />
          </IconButton>
        </Stack>
      </DialogTitle>

      <Tabs
        value={activeTab}
        onChange={(_, nextValue: SettingsTab) => setActiveTab(nextValue)}
        aria-label="Settings tabs"
        sx={{
          px: 2,
          borderBottom: `1px solid ${theme.palette.divider}`,
        }}
      >
        <Tab label="General" value="general" />
        <Tab label="Docker" value="docker" />
      </Tabs>

      <DialogContent sx={{ px: 3, py: 3 }}>
        {activeTab === "general" ? (
          <Stack
            sx={{ py: 1, display: "flex", flexDirection: "column", gap: 2 }}
          >
            <Typography variant="body1" fontWeight={600}>
              Appearance
            </Typography>

            <Stack
              direction="row"
              alignItems="center"
              justifyContent="space-between"
              sx={{
                p: 1.5,
                borderRadius: 1.5,
                border: `1px solid ${theme.palette.divider}`,
              }}
            >
              <Stack>
                <Typography variant="body2" fontWeight={600}>
                  Primary color
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  Change the app accent color.
                </Typography>
              </Stack>
              <NavbarCustomizer />
            </Stack>

            <ThemeColorsSection />
          </Stack>
        ) : (
          <DockerFolderSettingsSection />
        )}
      </DialogContent>
    </GeneralDialog>
  );
};

export default SettingsDialog;
