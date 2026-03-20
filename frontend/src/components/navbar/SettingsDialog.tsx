import { Icon } from "@iconify/react";
import { Tab, Tabs } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useState } from "react";

import DockerFolderSettingsSection from "./DockerFolderSettingsSection";
import NavbarCustomizer from "./NavbarCustomizer";
import ThemeColorsSection from "./ThemeColorsSection";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
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
      <AppDialogTitle
        style={{
          backgroundColor: theme.palette.background.paper,
          borderBottom: `1px solid ${theme.palette.divider}`,
          paddingTop: 6,
          paddingBottom: 6,
          paddingLeft: 8,
          paddingRight: 8,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(1),
          }}
        >
          <AppTypography
            variant="h6"
            style={{
              flexGrow: 1,
            }}
          >
            Settings
          </AppTypography>
          <AppIconButton
            size="small"
            onClick={handleClose}
            aria-label="Close settings"
          >
            <Icon icon="mdi:close" width={18} height={18} />
          </AppIconButton>
        </div>
      </AppDialogTitle>

      <Tabs
        value={activeTab}
        onChange={(_, nextValue: SettingsTab) => setActiveTab(nextValue)}
        aria-label="Settings tabs"
        style={{
          paddingLeft: 8,
          paddingRight: 8,
          borderBottom: "1px solid var(--color-divider)",
        }}
      >
        <Tab label="General" value="general" />
        <Tab label="Docker" value="docker" />
      </Tabs>

      <AppDialogContent
        style={{
          paddingLeft: 12,
          paddingRight: 12,
          paddingTop: 12,
          paddingBottom: 12,
        }}
      >
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
            <AppTypography variant="body1" fontWeight={600}>
              Appearance
            </AppTypography>

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
                <AppTypography variant="body2" fontWeight={600}>
                  Primary color
                </AppTypography>
                <AppTypography variant="caption" color="text.secondary">
                  Change the app accent color.
                </AppTypography>
              </div>
              <NavbarCustomizer />
            </div>

            <ThemeColorsSection />
          </div>
        ) : (
          <DockerFolderSettingsSection />
        )}
      </AppDialogContent>
    </GeneralDialog>
  );
};
export default SettingsDialog;
