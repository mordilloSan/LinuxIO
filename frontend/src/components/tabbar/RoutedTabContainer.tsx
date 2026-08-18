import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import {
  createContext,
  memo,
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

import type { FileRouteTypes } from "@/routeTree.gen";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import { getTabSelectorThemeVars } from "./TabSelector";
import AppIconButton from "../ui/AppIconButton";
import AppMenu from "../ui/AppMenu";
import AppPopover from "../ui/AppPopover";

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
  hasActiveSlotSearch?: boolean;
  hasSlotActions?: boolean;
  hasSlotSearch?: boolean;
  searchHostMountRef?: (element: HTMLDivElement | null) => void;
  tabs: readonly RoutedTab[];
}

const EMPTY_CONTAINER_STYLE: CSSProperties = {};

const TabActionSlotContext = createContext<{
  host: HTMLElement;
  registerActions: () => () => void;
} | null>(null);

const TabSearchSlotContext = createContext<{
  host: HTMLElement;
  registerActiveSearch: () => () => void;
  registerSearch: () => () => void;
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

/** Places a child route's search field in the persistent parent tab strip. */
export const RoutedTabSearch = ({
  active = false,
  children,
}: {
  /**
   * The field carries a query right now. While true, the collapsed mobile
   * icons take the theme accent so a filtered list isn't mistaken for the
   * full one.
   */
  active?: boolean;
  children: ReactNode;
}) => {
  const parentSearchSlot = useContext(TabSearchSlotContext);
  const hasSearch = children != null && typeof children !== "boolean";

  useLayoutEffect(() => {
    if (!parentSearchSlot || !hasSearch) return;
    return parentSearchSlot.registerSearch();
  }, [hasSearch, parentSearchSlot]);

  useLayoutEffect(() => {
    if (!parentSearchSlot || !hasSearch || !active) return;
    return parentSearchSlot.registerActiveSearch();
  }, [active, hasSearch, parentSearchSlot]);

  if (!hasSearch) {
    return null;
  }

  if (!parentSearchSlot) {
    return <>{children}</>;
  }

  return createPortal(children, parentSearchSlot.host);
};

export const RoutedTabLayout = ({
  children,
  containerStyle = EMPTY_CONTAINER_STYLE,
  tabs,
}: RoutedTabContainerProps) => {
  // Keep the portal targets for the layout's lifetime so moving route content
  // does not reset state held by its header controls.
  const [actionHost] = useState(() => {
    const host = document.createElement("div");
    host.className = "tab-selector__action-portal";
    return host;
  });
  const [searchHost] = useState(() => {
    const host = document.createElement("div");
    host.className = "tab-selector__search-portal";
    return host;
  });
  const [slotActionCount, setSlotActionCount] = useState(0);
  const [slotSearchCount, setSlotSearchCount] = useState(0);
  const [activeSearchCount, setActiveSearchCount] = useState(0);
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
  const registerSearch = useCallback(() => {
    setSlotSearchCount((count) => count + 1);
    return () => setSlotSearchCount((count) => Math.max(0, count - 1));
  }, []);
  const registerActiveSearch = useCallback(() => {
    setActiveSearchCount((count) => count + 1);
    return () => setActiveSearchCount((count) => Math.max(0, count - 1));
  }, []);
  const mountSearchHost = useCallback(
    (element: HTMLDivElement | null) => {
      if (element) {
        element.append(searchHost);
      }
    },
    [searchHost],
  );
  const actionSlot = useMemo(
    () => ({ host: actionHost, registerActions }),
    [actionHost, registerActions],
  );
  const searchSlot = useMemo(
    () => ({ host: searchHost, registerActiveSearch, registerSearch }),
    [registerActiveSearch, registerSearch, searchHost],
  );
  return (
    <div className="tab-container" style={containerStyle}>
      <TabSelector
        actionHostMountRef={mountActionHost}
        hasActiveSlotSearch={activeSearchCount > 0}
        hasSlotActions={slotActionCount > 0}
        hasSlotSearch={slotSearchCount > 0}
        searchHostMountRef={mountSearchHost}
        tabs={tabs}
      />
      <TabSearchSlotContext value={searchSlot}>
        <TabActionSlotContext value={actionSlot}>
          <TabPanel>{children}</TabPanel>
        </TabActionSlotContext>
      </TabSearchSlotContext>
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
  hasActiveSlotSearch = false,
  hasSlotActions = false,
  hasSlotSearch = false,
  searchHostMountRef,
}: TabSelectorProps) {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const [mobileSearchAnchorEl, setMobileSearchAnchorEl] =
    useState<HTMLElement | null>(null);
  const mobileSearchRef = useRef<HTMLDivElement | null>(null);
  const handleMenuTriggerRef = useCallback(
    (element: HTMLButtonElement | null) => {
      if (!element) {
        setAnchorEl(null);
        setMobileSearchAnchorEl(null);
      }
    },
    [],
  );
  const handleMobileSearchRef = useCallback(
    (element: HTMLDivElement | null) => {
      mobileSearchRef.current = element;
      searchHostMountRef?.(element);
      if (!element) {
        setMobileSearchAnchorEl(null);
      }
    },
    [searchHostMountRef],
  );

  useLayoutEffect(() => {
    if (!mobileSearchAnchorEl) return;

    mobileSearchRef.current
      ?.querySelector<HTMLInputElement | HTMLTextAreaElement>("input, textarea")
      ?.focus();
  }, [mobileSearchAnchorEl]);

  const handleMobileSearchClose = useCallback(() => {
    const focusedElement = document.activeElement;
    const trigger = mobileSearchAnchorEl;

    setMobileSearchAnchorEl(null);
    if (trigger && mobileSearchRef.current?.contains(focusedElement)) {
      // The search owned focus, so return it to its trigger. Ring presentation
      // is independent and appears only after Tab navigation begins.
      trigger.focus();
    }
  }, [mobileSearchAnchorEl]);

  const hasMobileActions = hasSlotActions || hasSlotSearch;

  return (
    <div
      className={`tab-selector tab-container__selector${isMobile ? " tab-selector--mobile" : ""}`}
      style={getTabSelectorThemeVars(theme)}
    >
      <div className="tab-selector__scroller custom-scrollbar">
        <div aria-label="Tabs" className="tab-selector__pills" role="tablist">
          {tabs.map((tab) => (
            <RoutedTabLink key={tab.to} {...tab} />
          ))}
        </div>
      </div>

      {!isMobile && searchHostMountRef && hasSlotSearch ? (
        <div className="tab-selector__search" ref={searchHostMountRef} />
      ) : null}

      {isMobile ? (
        hasMobileActions ? (
          <>
            <AppIconButton
              aria-expanded={Boolean(anchorEl)}
              aria-label="Actions"
              className={
                hasActiveSlotSearch ? "tab-selector__search-active" : undefined
              }
              onClick={(event) => {
                setMobileSearchAnchorEl(null);
                setAnchorEl(event.currentTarget);
              }}
              ref={handleMenuTriggerRef}
              size="small"
              style={{
                gridColumn: 2,
                justifySelf: "end",
                marginTop: 2,
                flexShrink: 0,
              }}
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
              <div className="tab-selector__mobile-actions">
                {hasSlotSearch ? (
                  <AppIconButton
                    aria-label="Search"
                    className={
                      hasActiveSlotSearch
                        ? "tab-selector__search-active"
                        : undefined
                    }
                    onClick={() => {
                      setMobileSearchAnchorEl(anchorEl);
                      setAnchorEl(null);
                    }}
                    size="small"
                  >
                    <Icon height={20} icon="mdi:magnify" width={20} />
                  </AppIconButton>
                ) : null}
                {actionHostMountRef && hasSlotActions ? (
                  <div ref={actionHostMountRef} />
                ) : null}
              </div>
            </AppMenu>
            {searchHostMountRef && hasSlotSearch ? (
              <AppPopover
                anchorEl={mobileSearchAnchorEl}
                anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
                keepMounted
                onClose={handleMobileSearchClose}
                open={Boolean(mobileSearchAnchorEl)}
                paperClassName="tab-selector__mobile-search-popover"
                transformOrigin={{ vertical: "top", horizontal: "right" }}
              >
                <div
                  className="tab-selector__mobile-search"
                  ref={handleMobileSearchRef}
                  role="search"
                />
              </AppPopover>
            ) : null}
          </>
        ) : null
      ) : actionHostMountRef && hasSlotActions ? (
        <div className="tab-selector__actions" ref={actionHostMountRef} />
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
