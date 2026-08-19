import { useState } from "react";

import type { DockTileColors, Theme } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import TabSelector from "@/components/tabbar/TabSelector";
import AppDivider from "@/components/ui/AppDivider";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useConfigValue } from "@/hooks/useConfig";
import CapabilityManagerSection from "@/routes/_authenticated/-components/navbar/CapabilityManagerSection";
import DockerSettingsSection from "@/routes/_authenticated/-components/navbar/DockerSettingsSection";
import IndexerSettingsSection from "@/routes/_authenticated/-components/navbar/IndexerSettingsSection";
import MonitoringSettingsSection from "@/routes/_authenticated/-components/navbar/MonitoringSettingsSection";
import NavbarCustomizer from "@/routes/_authenticated/-components/navbar/NavbarCustomizer";
import PowerSettingsSection from "@/routes/_authenticated/-components/navbar/PowerSettingsSection";
import ThemeColorsSection from "@/routes/_authenticated/-components/navbar/ThemeColorsSection";
import UpdateSettings, {
  useUpdateSettingsState,
} from "@/routes/_authenticated/updates/-components/UpdateSettings";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { getDialogSurfaceStyles } from "@/theme/surfaces";

import DockAccentGradientEditor from "./DockAccentGradientEditor";
import "./settings-page.css";

type SettingsTab =
  | "general"
  | "updates"
  | "theme"
  | "capabilities"
  | "docker"
  | "indexer"
  | "monitoring"
  | "power";

// Typed as Theme rather than inferred, so the pills stay in step with the
// backend's validated enum instead of widening to string.
const THEME_MODE_OPTIONS: readonly { label: string; value: Theme }[] = [
  { label: "Light", value: "LIGHT" },
  { label: "Dark", value: "DARK" },
];

const SettingsPage = () => {
  const theme = useAppTheme();
  const isDesktop = useAppMediaQuery(theme.breakpoints.up("md"));
  const { privileged } = useAuth();
  const [themeMode, setThemeMode] = useConfigValue("theme");
  const [navigationMode, setNavigationMode] = useConfigValue("navigationMode");
  const [dockTileColors, setDockTileColors] = useConfigValue("dockTileColors");
  const [dockAccentGradient, setDockAccentGradient] =
    useConfigValue("dockAccentGradient");
  const [activeTab, setActiveTab] = useState<SettingsTab>("general");
  const effectiveTab =
    !privileged &&
    (activeTab === "power" ||
      activeTab === "indexer" ||
      activeTab === "monitoring")
      ? "general"
      : activeTab;
  /* Polls list_timers every 5s while it is on, so it stays off until its own
     tab is the one being looked at. */
  const updateSettingsState = useUpdateSettingsState(
    effectiveTab === "updates",
  );
  const tabs = [
    { value: "general", label: "General" },
    { value: "updates", label: "Updates" },
    { value: "theme", label: "Theme" },
    { value: "capabilities", label: "Capabilities" },
    { value: "docker", label: "Docker" },
    ...(privileged ? [{ value: "indexer", label: "Indexer" }] : []),
    ...(privileged ? [{ value: "monitoring", label: "Monitoring" }] : []),
    ...(privileged ? [{ value: "power", label: "Power" }] : []),
  ];

  const sectionErrorFallback = (
    <div style={{ padding: theme.spacing(1) }}>
      <AppTypography color="error">
        This settings section failed to render.
      </AppTypography>
    </div>
  );

  return (
    <div className="settings-page">
      <div
        className="settings-page__sheet"
        style={getDialogSurfaceStyles(theme)}
      >
        <div className="settings-page__header">
          <AppTypography variant="h3">Settings</AppTypography>
        </div>

        <div className="settings-page__tabs">
          <TabSelector
            onChange={(nextValue) => setActiveTab(nextValue as SettingsTab)}
            options={tabs}
            style={{ marginBottom: 0 }}
            value={effectiveTab}
          />
          <AppDivider />
        </div>

        <div className="settings-page__body custom-scrollbar">
          <ErrorBoundary key={effectiveTab} fallback={sectionErrorFallback}>
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
                  style={{
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

                <FrostedCard
                  hoverLift
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: theme.spacing(1.5),
                  }}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <AppTypography fontWeight={600} variant="body2">
                      Theme mode
                    </AppTypography>
                    <AppTypography color="text.secondary" variant="caption">
                      Each mode keeps its own palette, editable under Theme.
                    </AppTypography>
                  </div>
                  <TabSelector
                    onChange={(value) => setThemeMode(value)}
                    options={THEME_MODE_OPTIONS}
                    style={{
                      flexShrink: 0,
                      gridTemplateColumns: "max-content",
                      marginBottom: 0,
                      marginLeft: theme.spacing(1.5),
                      width: "max-content",
                    }}
                    value={themeMode}
                  />
                </FrostedCard>

                <FrostedCard
                  hoverLift
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: theme.spacing(1.5),
                  }}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <AppTypography fontWeight={600} variant="body2">
                      Navigation style
                    </AppTypography>
                    <AppTypography color="text.secondary" variant="caption">
                      Classic sidebar, or a macOS-style dock in the header.
                    </AppTypography>
                  </div>
                  <TabSelector
                    onChange={(value) => setNavigationMode(value)}
                    options={[
                      { label: "Sidebar", value: "sidebar" },
                      ...(isDesktop
                        ? [{ label: "Dock", value: "dock" } as const]
                        : []),
                    ]}
                    style={{
                      flexShrink: 0,
                      gridTemplateColumns: "max-content",
                      marginBottom: 0,
                      marginLeft: theme.spacing(1.5),
                      width: "max-content",
                    }}
                    value={
                      isDesktop ? (navigationMode ?? "sidebar") : "sidebar"
                    }
                  />
                </FrostedCard>

                <FrostedCard
                  className="settings-page__dock-colors-card"
                  style={{ padding: theme.spacing(1.5) }}
                >
                  <div className="settings-page__dock-colors-header">
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <AppTypography fontWeight={600} variant="body2">
                        Dock tile colors
                      </AppTypography>
                      <AppTypography color="text.secondary" variant="caption">
                        Choose a coordinated palette or a distinct color per
                        app.
                      </AppTypography>
                    </div>
                    <AppSelect
                      className="settings-page__dock-colors-select"
                      onChange={(event) =>
                        setDockTileColors(event.target.value as DockTileColors)
                      }
                      size="small"
                      value={dockTileColors ?? "accent"}
                    >
                      <option value="accent">Accent family</option>
                      <option value="mono">Single accent</option>
                      <option value="neutral">Neutral, accent on active</option>
                      <option value="vibrant">Vibrant (per app)</option>
                    </AppSelect>
                  </div>

                  {(dockTileColors ?? "accent") === "accent" ? (
                    <DockAccentGradientEditor
                      accent={theme.palette.primary.main}
                      onChange={setDockAccentGradient}
                      value={dockAccentGradient}
                    />
                  ) : null}
                </FrostedCard>
              </div>
            ) : null}
            {effectiveTab === "updates" ? (
              <div
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: theme.spacing(1.5),
                }}
              >
                <div>
                  <AppTypography fontWeight={600} variant="body1">
                    Automatic Updates
                  </AppTypography>
                  <AppTypography color="text.secondary" variant="caption">
                    When the system downloads and installs package updates on
                    its own.
                  </AppTypography>
                </div>

                <UpdateSettings disablePadding state={updateSettingsState} />
              </div>
            ) : null}
            {effectiveTab === "theme" ? <ThemeColorsSection /> : null}
            {effectiveTab === "capabilities" ? (
              <CapabilityManagerSection />
            ) : null}
            {effectiveTab === "docker" ? <DockerSettingsSection /> : null}
            {effectiveTab === "indexer" ? <IndexerSettingsSection /> : null}
            {effectiveTab === "monitoring" ? (
              <MonitoringSettingsSection />
            ) : null}
            {effectiveTab === "power" ? <PowerSettingsSection /> : null}
          </ErrorBoundary>
        </div>
      </div>
    </div>
  );
};

export default SettingsPage;
