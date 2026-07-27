import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import { useState, type CSSProperties, type ReactNode } from "react";

import type { FileRouteTypes } from "@/routeTree.gen";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import AppIconButton from "../ui/AppIconButton";
import AppMenu from "../ui/AppMenu";

import "./tab-container.css";
import "./tab-panel.css";
import "./tab-selector.css";

type RoutedTabTarget = Exclude<FileRouteTypes["to"], "/filebrowser/$">;

export interface RoutedTab {
  label: string;
  to: RoutedTabTarget;
}

interface RoutedTabContainerProps {
  children: ReactNode;
  containerStyle?: CSSProperties;
  rightContent?: ReactNode;
  tabs: readonly RoutedTab[];
}

/**
 * Page-level tab layout backed by real child-route links.
 *
 * Routed content deliberately relies on the route error boundary rather than
 * the legacy TabPanel catch-all boundary.
 */
const RoutedTabContainer = ({
  children,
  containerStyle = {},
  rightContent,
  tabs,
}: RoutedTabContainerProps) => {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);

  return (
    <div className="tab-container" style={containerStyle}>
      <div
        className="tab-selector tab-container__selector"
        style={
          {
            "--tab-selector-active-bg": theme.palette.primary.main,
            "--tab-selector-active-color": theme.palette.primary.contrastText,
            "--tab-selector-border": theme.palette.divider,
            "--tab-selector-hover": theme.palette.action.hover,
            "--tab-selector-text": theme.palette.text.secondary,
          } as CSSProperties
        }
      >
        <div className="tab-selector__scroller custom-scrollbar">
          <div aria-label="Tabs" className="tab-selector__pills" role="tablist">
            {tabs.map((tab) => (
              <Link
                activeOptions={{ exact: true, includeSearch: false }}
                activeProps={{
                  "aria-selected": true,
                  className: "tab-selector__pill--active",
                }}
                className="tab-selector__pill"
                inactiveProps={{ "aria-selected": false }}
                key={tab.to}
                role="tab"
                to={tab.to}
              >
                {tab.label}
              </Link>
            ))}
          </div>
        </div>

        {rightContent &&
          (isMobile ? (
            <>
              <AppIconButton
                onClick={(event) => setAnchorEl(event.currentTarget)}
                size="small"
                style={{ marginTop: 2, flexShrink: 0 }}
              >
                <Icon height={20} icon="mdi:tune" width={20} />
              </AppIconButton>
              <AppMenu
                anchorEl={anchorEl}
                anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
                minWidth="unset"
                onClose={() => setAnchorEl(null)}
                open={Boolean(anchorEl)}
                transformOrigin={{ vertical: "top", horizontal: "right" }}
              >
                <div className="tab-selector__mobile-actions">
                  {rightContent}
                </div>
              </AppMenu>
            </>
          ) : (
            <div className="tab-selector__actions">{rightContent}</div>
          ))}
      </div>

      <div className="tab-container__panels">
        <div className="tab-panel">
          <div className="tab-panel__content">{children}</div>
        </div>
      </div>
    </div>
  );
};

export default RoutedTabContainer;
