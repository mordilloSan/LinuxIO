import { Icon } from "@iconify/react";
import React, { useState } from "react";

import DockerFolderSettingsSection from "./DockerFolderSettingsSection";
import NavbarCustomizer from "./NavbarCustomizer";
import PowerSettingsSection from "./PowerSettingsSection";
import ThemeColorsSection from "./ThemeColorsSection";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import TabSelector from "@/components/tabbar/TabSelector";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useAppTheme } from "@/theme";
type SettingsTab = "general" | "docker" | "power";
interface SettingsDialogProps {
  open: boolean;
  onClose: () => void;
}
const SettingsDialog: React.FC<SettingsDialogProps> = ({ open, onClose }) => {
  const theme = useAppTheme();
  const { privileged } = useAuth();
  const baseBorderRadius = parseFloat(String(theme.shape.borderRadius)) || 0;
  const [activeTab, setActiveTab] = useState<SettingsTab>("general");
  const effectiveTab =
    !privileged && activeTab === "power" ? "general" : activeTab;
  const tabs = [
    { value: "general", label: "General" },
    { value: "docker", label: "Docker" },
    ...(privileged ? [{ value: "power", label: "Power" }] : []),
  ];

  const handleClose = () => {
    setActiveTab("general");
    onClose();
  };
  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="md" fullWidth>
      <AppDialogTitle
        style={{
          backgroundColor: theme.palette.background.paper,
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
            justifyContent: "center",
            position: "relative",
          }}
        >
          <AppTypography variant="h5">Settings</AppTypography>
          <AppIconButton
            size="small"
            onClick={handleClose}
            aria-label="Close settings"
            style={{ position: "absolute", right: 0 }}
          >
            <Icon icon="mdi:close" width={18} height={18} />
          </AppIconButton>
        </div>
      </AppDialogTitle>

      <div
        style={{
          paddingLeft: 8,
          paddingRight: 8,
        }}
      >
        <TabSelector
          value={effectiveTab}
          onChange={(nextValue) => setActiveTab(nextValue as SettingsTab)}
          options={tabs}
          style={{ marginBottom: 0 }}
        />
      </div>

      <AppDialogContent
        style={{
          paddingLeft: 12,
          paddingRight: 12,
          paddingTop: 12,
          paddingBottom: 12,
        }}
      >
        {effectiveTab === "general" ? (
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
        ) : null}
        {effectiveTab === "docker" ? <DockerFolderSettingsSection /> : null}
        {effectiveTab === "power" ? <PowerSettingsSection /> : null}
      </AppDialogContent>
    </GeneralDialog>
  );
};
export default SettingsDialog;
