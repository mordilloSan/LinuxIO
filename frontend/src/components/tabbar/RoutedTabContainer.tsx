import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import {
  createContext,
  memo,
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

import type { FileRouteTypes } from "@/routeTree.gen";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import AppIconButton from "../ui/AppIconButton";
import AppMenu from "../ui/AppMenu";

import "./tab-container.css";
import "./tab-panel.css";
import "./tab-selector.css";

// Tabs render a bare <Link to>, so any route needing path params (splat or
// named) cannot be a tab target. Matching on "$" keeps this true for routes
// added later without editing this list.
type RoutedTabTarget = Exclude<FileRouteTypes["to"], `${string}$${string}`>;

export interface RoutedTab {
  label: string;
  /**
   * Keep this pill selected while one of its child routes is active. Set it on
   * a tab that owns nested detail routes; leave it off for leaf tabs so a
   * parent path does not stay selected on its siblings.
   */
  matchChildren?: boolean;
  to: RoutedTabTarget;
}

interface RoutedTabContainerProps {
  children?: ReactNode;
  containerStyle?: CSSProperties;
  tabs: readonly RoutedTab[];
}

interface TabSelectorProps {
  actionHostMountRef?: (element: HTMLDivElement | null) => void;
  hasSlotActions?: boolean;
  tabs: readonly RoutedTab[];
}

const EMPTY_CONTAINER_STYLE: CSSProperties = {};

const TabActionSlotContext = createContext<{
  host: HTMLElement;
  registerActions: () => () => void;
} | null>(null);

/**
 * Places child-route actions in the persistent parent tab strip. This keeps
 * the strip (and its error boundary) mounted while the child route changes.
 */
export const RoutedTabActions = ({ children }: { children: ReactNode }) => {
  const parentActionSlot = useContext(TabActionSlotContext);
  // Booleans cover the `{condition && <Action />}` pattern, which renders
  // nothing but would otherwise still register a ghost mobile menu.
  const hasActions = children != null && typeof children !== "boolean";

  useLayoutEffect(() => {
    if (!parentActionSlot || !hasActions) return;
    return parentActionSlot.registerActions();
  }, [hasActions, parentActionSlot]);

  if (!hasActions) {
    return null;
  }

  if (!parentActionSlot) {
    return <>{children}</>;
  }

  return createPortal(children, parentActionSlot.host);
};

export const RoutedTabLayout = ({
  children,
  containerStyle = EMPTY_CONTAINER_STYLE,
  tabs,
}: RoutedTabContainerProps) => {
  // Keep one portal target for the layout's lifetime. Moving this element
  // between desktop and mobile mounts preserves state inside child actions.
  const [actionHost] = useState(() => {
    const host = document.createElement("div");
    host.className = "tab-selector__action-portal";
    return host;
  });
  const [slotActionCount, setSlotActionCount] = useState(0);
  const registerActions = useCallback(() => {
    setSlotActionCount((count) => count + 1);
    return () => setSlotActionCount((count) => Math.max(0, count - 1));
  }, []);
  const mountActionHost = useCallback(
    (element: HTMLDivElement | null) => {
      if (element) {
        element.append(actionHost);
      }
    },
    [actionHost],
  );
  const actionSlot = useMemo(
    () => ({ host: actionHost, registerActions }),
    [actionHost, registerActions],
  );
  return (
    <div className="tab-container" style={containerStyle}>
      <TabSelector
        actionHostMountRef={mountActionHost}
        hasSlotActions={slotActionCount > 0}
        tabs={tabs}
      />
      <TabActionSlotContext value={actionSlot}>
        <TabPanel>{children}</TabPanel>
      </TabActionSlotContext>
    </div>
  );
};

const RoutedTabLink = memo(function RoutedTabLink({
  label,
  matchChildren,
  to,
}: RoutedTab) {
  return (
    <Link
      activeOptions={{
        exact: !matchChildren,
        includeSearch: false,
      }}
      activeProps={{
        "aria-selected": true,
        className: "tab-selector__pill--active",
      }}
      className="tab-selector__pill"
      inactiveProps={{ "aria-selected": false }}
      role="tab"
      to={to}
    >
      {label}
    </Link>
  );
});

const TabSelector = memo(function TabSelector({
  tabs,
  actionHostMountRef,
  hasSlotActions = false,
}: TabSelectorProps) {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const handleMenuTriggerRef = useCallback(
    (element: HTMLButtonElement | null) => {
      if (!element) {
        setAnchorEl(null);
      }
    },
    [],
  );

  return (
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
            <RoutedTabLink key={tab.to} {...tab} />
          ))}
        </div>
      </div>

      {actionHostMountRef && hasSlotActions ? (
        isMobile ? (
          <>
            <AppIconButton
              aria-expanded={Boolean(anchorEl)}
              aria-label="Actions"
              onClick={(event) => setAnchorEl(event.currentTarget)}
              ref={handleMenuTriggerRef}
              size="small"
              style={{ marginTop: 2, flexShrink: 0 }}
            >
              <Icon height={20} icon="mdi:tune" width={20} />
            </AppIconButton>
            <AppMenu
              anchorEl={anchorEl}
              anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
              keepMounted
              minWidth="unset"
              onClose={() => setAnchorEl(null)}
              open={Boolean(anchorEl)}
              transformOrigin={{ vertical: "top", horizontal: "right" }}
            >
              <div
                className="tab-selector__mobile-actions"
                ref={actionHostMountRef}
              />
            </AppMenu>
          </>
        ) : (
          <div className="tab-selector__actions" ref={actionHostMountRef} />
        )
      ) : null}
    </div>
  );
});

const TabPanel = ({ children }: { children: ReactNode }) => (
  <div className="tab-container__panels">
    <div className="tab-panel">
      <div className="tab-panel__content">{children}</div>
    </div>
  </div>
);
