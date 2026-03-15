import { Icon } from "@iconify/react";
import {
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
import ThemeColorsSection from "./ThemeColorsSection";

import GeneralDialog from "@/components/dialog/GeneralDialog";

type SettingsTab = "general" | "docker";

interface SettingsDialogProps {
  open: boolean;
  onClose: () => void;
}

const SettingsDialog: React.FC<SettingsDialogProps> = ({ open, onClose }) => {
  const theme = useTheme();
  const baseBorderRadius = parseFloat(String(theme.shape.borderRadius)) || 0;
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
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(1),
          }}
        >
          <Typography variant="h6" sx={{ flexGrow: 1 }}>
            Settings
          </Typography>
          <IconButton
            size="small"
            onClick={handleClose}
            aria-label="Close settings"
          >
            <Icon icon="mdi:close" width={18} height={18} />
          </IconButton>
        </div>
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
          <div
            style={{
              paddingTop: theme.spacing(1),
              paddingBottom: theme.spacing(1),
              display: "flex",
              flexDirection: "column",
              gap: theme.spacing(2),
            }}
          >
            <Typography variant="body1" fontWeight={600}>
              Appearance
            </Typography>

            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                padding: theme.spacing(1.5),
                borderRadius: `${baseBorderRadius * 1.5}px`,
                border: `1px solid ${theme.palette.divider}`,
              }}
            >
              <div>
                <Typography variant="body2" fontWeight={600}>
                  Primary color
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  Change the app accent color.
                </Typography>
              </div>
              <NavbarCustomizer />
            </div>

            <ThemeColorsSection />
          </div>
        ) : (
          <DockerFolderSettingsSection />
        )}
      </DialogContent>
    </GeneralDialog>
  );
};

export default SettingsDialog;
