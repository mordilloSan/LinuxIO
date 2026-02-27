import CloseIcon from "@mui/icons-material/Close";
import {
  Box,
  DialogContent,
  DialogTitle,
  IconButton,
  Tab,
  Tabs,
  Typography,
  useTheme,
} from "@mui/material";
import React, { useState } from "react";

import DockerFolderSettingsSection from "./DockerFolderSettingsSection";
import NavbarCustomizer from "./NavbarCustomizer";

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
          backgroundColor: theme.header.background,
          borderBottom: `1px solid ${theme.palette.divider}`,
          py: 1.5,
          px: 2,
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
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
        </Box>
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
          <Box sx={{ py: 1, display: "flex", flexDirection: "column", gap: 2 }}>
            <Typography variant="body1" fontWeight={600}>
              Appearance
            </Typography>

            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                p: 1.5,
                borderRadius: 1.5,
                border: `1px solid ${theme.palette.divider}`,
              }}
            >
              <Box>
                <Typography variant="body2" fontWeight={600}>
                  Primary color
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  Change the app accent color.
                </Typography>
              </Box>
              <NavbarCustomizer />
            </Box>

            <Typography variant="body2" color="text.secondary">
              Additional general settings will be added in a future iteration.
            </Typography>
          </Box>
        ) : (
          <DockerFolderSettingsSection />
        )}
      </DialogContent>
    </GeneralDialog>
  );
};

export default SettingsDialog;
