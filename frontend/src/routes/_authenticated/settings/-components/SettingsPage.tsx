import { getRouteApi } from "@tanstack/react-router";

import type { DockTileColors, NavigationMode, Theme } from "@/api";
import { cardBodyCycleProps } from "@/components/cards/cardBodyToggle";
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
import { useAppMediaQuery } from "@/theme";
import { up } from "@/theme/breakpoints";
import { getDialogSurfaceStyles } from "@/theme/surfaces";

import DockAccentGradientEditor from "./DockAccentGradientEditor";
import {
  DEFAULT_SETTINGS_TAB,
  PRIVILEGED_SETTINGS_TABS,
  SETTINGS_TABS,
} from "./settingsTabs";
import "./settings-page.css";

const settingsRouteApi = getRouteApi("/_authenticated/settings");

// Typed as Theme rather than inferred, so the pills stay in step with the
// backend's validated enum instead of widening to string.
const THEME_MODE_OPTIONS: readonly { label: string; value: Theme }[] = [
  { label: "Light", value: "LIGHT" },
  { label: "Dark", value: "DARK" },
];

const THEME_MODE_VALUES = THEME_MODE_OPTIONS.map((option) => option.value);

const NAVIGATION_MODE_OPTIONS: readonly {
  label: string;
  value: NavigationMode;
}[] = [
  { label: "Sidebar", value: "sidebar" },
  { label: "Dock", value: "dock" },
];

const SettingsPage = () => {
  const isDesktop = useAppMediaQuery(up("md"));
  const { privileged } = useAuth();
  const [themeMode, setThemeMode] = useConfigValue("theme");
  const [navigationMode, setNavigationMode] = useConfigValue("navigationMode");
  const [dockTileColors, setDockTileColors] = useConfigValue("dockTileColors");
  const dockColorMode = dockTileColors;
  // The dock needs the header width only a desktop viewport gives it, so on a
  // phone the sidebar is the only choice — and the one choice is not a toggle.
  const navigationOptions = isDesktop
    ? NAVIGATION_MODE_OPTIONS
    : NAVIGATION_MODE_OPTIONS.filter((option) => option.value === "sidebar");
  const navigationValue: NavigationMode = isDesktop
    ? navigationMode
    : "sidebar";
  const [dockAccentGradient, setDockAccentGradient] =
    useConfigValue("dockAccentGradient");
  const navigate = settingsRouteApi.useNavigate();
  const { tab } = settingsRouteApi.useSearch();
  const activeTab = tab ?? DEFAULT_SETTINGS_TAB;
  // A link into a privileged tab still opens for a session that cannot read it,
  // so the fall back stays here rather than in the route's validator.
  const effectiveTab =
    !privileged && PRIVILEGED_SETTINGS_TABS.includes(activeTab)
      ? DEFAULT_SETTINGS_TAB
      : activeTab;
  /* Polls list_timers every 5s while it is on, so it stays off until its own
     tab is the one being looked at. */
  const updateSettingsState = useUpdateSettingsState(
    effectiveTab === "updates",
  );
  const tabs = privileged
    ? SETTINGS_TABS
    : SETTINGS_TABS.filter(
        (option) => !PRIVILEGED_SETTINGS_TABS.includes(option.value),
      );

  const sectionErrorFallback = (
    <div style={{ padding: "var(--app-space-4)" }}>
      <AppTypography color="error">
        This settings section failed to render.
      </AppTypography>
    </div>
  );

  return (
    <div className="settings-page">
      <div className="settings-page__sheet" style={getDialogSurfaceStyles()}>
        <div className="settings-page__header">
          <AppTypography variant="h3">Settings</AppTypography>
        </div>

        <div className="settings-page__tabs">
          <TabSelector
            onChange={(nextValue) => {
              void navigate({
                search: (previous) => ({
                  ...previous,
                  // General is what /settings already means, so it is the one
                  // tab that leaves no parameter behind.
                  tab:
                    nextValue === DEFAULT_SETTINGS_TAB ? undefined : nextValue,
                }),
              });
            }}
            options={tabs}
            style={{ marginBottom: 0 }}
            value={effectiveTab}
          />
          <AppDivider />
        </div>

        <div className="settings-page__body">
          <ErrorBoundary key={effectiveTab} fallback={sectionErrorFallback}>
            {effectiveTab === "general" ? (
              <div
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: "var(--app-space-6)",
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
                    padding: "var(--app-space-6)",
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
                  {...cardBodyCycleProps({
                    onChange: (value) => setThemeMode(value),
                    value: themeMode,
                    values: THEME_MODE_VALUES,
                  })}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "var(--app-space-6)",
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
                      marginLeft: "var(--app-space-6)",
                      width: "max-content",
                    }}
                    value={themeMode}
                  />
                </FrostedCard>

                <FrostedCard
                  hoverLift
                  {...cardBodyCycleProps({
                    onChange: (value) => setNavigationMode(value),
                    value: navigationValue,
                    values: navigationOptions.map((option) => option.value),
                  })}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "var(--app-space-6)",
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
                    options={navigationOptions}
                    style={{
                      flexShrink: 0,
                      gridTemplateColumns: "max-content",
                      marginBottom: 0,
                      marginLeft: "var(--app-space-6)",
                      width: "max-content",
                    }}
                    value={navigationValue}
                  />
                </FrostedCard>

                <FrostedCard
                  className="settings-page__dock-colors-card"
                  // A row like the three above it, until the accent palette
                  // grows a gradient editor inside it — a card that rises out
                  // from under a drag in progress is worse than one that sits
                  // still, so the lift goes away while that editor is open.
                  hoverLift={dockColorMode !== "accent"}
                  style={{ padding: "var(--app-space-6)" }}
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
                      value={dockColorMode}
                    >
                      <option value="accent">Accent family</option>
                      <option value="mono">Single accent</option>
                      <option value="neutral">Neutral, accent on active</option>
                      <option value="vibrant">Vibrant (per app)</option>
                    </AppSelect>
                  </div>

                  {dockColorMode === "accent" ? (
                    <DockAccentGradientEditor
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
                  gap: "var(--app-space-6)",
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
