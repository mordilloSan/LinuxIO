import { Icon } from "@iconify/react";
import React, { useState } from "react";

import CapabilityManagerSection from "./CapabilityManagerSection";
import DockerFolderSettingsSection from "./DockerFolderSettingsSection";
import IndexerSettingsSection from "./IndexerSettingsSection";
import NavbarCustomizer from "./NavbarCustomizer";
import PowerSettingsSection from "./PowerSettingsSection";
import ThemeColorsSection from "./ThemeColorsSection";

import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import TabSelector from "@/components/tabbar/TabSelector";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppDivider from "@/components/ui/AppDivider";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useAppTheme } from "@/theme";

type SettingsTab =
  | "general"
  | "theme"
  | "capabilities"
  | "docker"
  | "indexer"
  | "power";
interface SettingsDialogProps {
  onClose: () => void;
  open: boolean;
}
const SettingsDialog: React.FC<SettingsDialogProps> = ({ open, onClose }) => {
  const theme = useAppTheme();
  const { privileged } = useAuth();
  const [activeTab, setActiveTab] = useState<SettingsTab>("general");
  const effectiveTab =
    !privileged && (activeTab === "power" || activeTab === "indexer")
      ? "general"
      : activeTab;
  const tabs = [
    { value: "general", label: "General" },
    { value: "theme", label: "Theme" },
    { value: "capabilities", label: "Capabilities" },
    { value: "docker", label: "Docker" },
    ...(privileged ? [{ value: "indexer", label: "Indexer" }] : []),
    ...(privileged ? [{ value: "power", label: "Power" }] : []),
  ];

  const handleClose = () => {
    setActiveTab("general");
    onClose();
  };
  return (
    <GeneralDialog
      fullWidth
      maxWidth="md"
      onClose={handleClose}
      open={open}
      style={{ alignSelf: "flex-start" }}
    >
      <AppDialogTitle
        style={{
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
          <AppTypography style={{ marginTop: 8 }} variant="h3">
            Settings
          </AppTypography>
          <AppIconButton
            aria-label="Close settings"
            onClick={handleClose}
            size="small"
            style={{ position: "absolute", right: 0 }}
          >
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
        </div>
      </AppDialogTitle>

      <div
        style={{
          paddingLeft: 12,
          paddingRight: 12,
        }}
      >
        <TabSelector
          onChange={(nextValue) => setActiveTab(nextValue as SettingsTab)}
          options={tabs}
          style={{ marginBottom: 0 }}
          value={effectiveTab}
        />
        <AppDivider />
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
              display: "flex",
              flexDirection: "column",
              gap: theme.spacing(1.5),
            }}
          >
            <div>
              <AppTypography fontWeight={600} variant="body1">
                General
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Common app preferences.
              </AppTypography>
            </div>

            <FrostedCard
              hoverLift
              onClick={(e) => {
                const target = e.target as HTMLElement;
                if (target.closest("button, input")) return;
                (e.currentTarget as HTMLElement)
                  .querySelector<HTMLButtonElement>("button")
                  ?.click();
              }}
              style={{
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                padding: theme.spacing(1.5),
              }}
            >
              <div>
                <AppTypography fontWeight={600} variant="body2">
                  Primary color
                </AppTypography>
                <AppTypography color="text.secondary" variant="caption">
                  Change the app accent color.
                </AppTypography>
              </div>
              <NavbarCustomizer />
            </FrostedCard>
          </div>
        ) : null}
        {effectiveTab === "theme" ? <ThemeColorsSection /> : null}
        {effectiveTab === "capabilities" ? <CapabilityManagerSection /> : null}
        {effectiveTab === "docker" ? <DockerFolderSettingsSection /> : null}
        {effectiveTab === "indexer" ? <IndexerSettingsSection /> : null}
        {effectiveTab === "power" ? <PowerSettingsSection /> : null}
      </AppDialogContent>
    </GeneralDialog>
  );
};
export default SettingsDialog;
