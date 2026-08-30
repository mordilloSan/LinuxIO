import { Outlet, useLocation } from "@tanstack/react-router";
import type { CSSProperties } from "react";

import BootstrapLoaderReady from "@/components/loaders/BootstrapLoaderReady";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import "@/icons/icons";
import { useConfigStorageMode, useConfigValue } from "@/hooks/useConfig";
import { useAppMediaQuery } from "@/theme";
import { up } from "@/theme/breakpoints";

import Footer from "./footer/Footer";
import Navbar from "./navbar/Navbar";
import Sidebar from "./sidebar/Sidebar";
import { useCloseMobileSidebarOnNavigate } from "./sidebar/useCloseMobileSidebarOnNavigate";
import useSidebar from "./sidebar/useSidebar";
import { useSidebarItems } from "./sidebar/useSidebarItems";
import UpdateBanner from "./update/UpdateBanner";
import { useUpdateInfo } from "./update/useUpdateInfo";

const MainLayout = () => {
  const location = useLocation();
  const isSmallUp = useAppMediaQuery(up("sm"));
  const [navigationMode] = useConfigValue("navigationMode");
  const storageMode = useConfigStorageMode();
  const { toggleMobileOpen, sidebarWidth, isDesktop } = useSidebar();
  const dockMode = isDesktop && navigationMode === "dock";
  const { updateInfo, dismissUpdate } = useUpdateInfo();
  const sidebarItems = useSidebarItems();

  useCloseMobileSidebarOnNavigate();

  /*
   * The gap between the header and the first thing on the page. A routed tab
   * strip reclaims it — and the inline padding, handed down as
   * --page-inset-inline — to sit flush under the header as a full-bleed bar
   * with the same geometry as the file browser header; see
   * components/tabbar/tab-container.css. Routes without that strip just get
   * the padding.
   */
  const pageInsetBlockStart =
    location.pathname === "/" || location.pathname.includes("/filebrowser")
      ? "0"
      : "var(--app-space-20)";

  const pageInsetInline = isSmallUp
    ? "var(--app-space-20)"
    : "var(--app-space-16)";

  const contentSpacing =
    location.pathname === "/"
      ? {
          paddingLeft: pageInsetInline,
          paddingRight: pageInsetInline,
          paddingTop: 0,
          paddingBottom: 0,
        }
      : location.pathname.includes("/filebrowser")
        ? { padding: 0 }
        : {
            paddingLeft: pageInsetInline,
            paddingRight: pageInsetInline,
            paddingTop: pageInsetBlockStart,
            paddingBottom: "var(--app-space-20)",
          };

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100dvh",
        minHeight: "100vh",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          display: "flex",
          flex: 1,
          minHeight: 0,
          minWidth: 0,
          overflow: "hidden",
        }}
      >
        {!dockMode && <Sidebar items={sidebarItems} />}
        <div
          style={{
            flex: 1,
            display: "flex",
            flexDirection: "column",
            minHeight: 0,
            minWidth: 0,
            transition:
              "margin-left var(--app-transition-duration-fast) var(--app-easing-standard), width var(--app-transition-duration-fast) var(--app-easing-standard)",
            marginLeft:
              !dockMode && isDesktop ? `${sidebarWidth}px` : undefined,
            width:
              !dockMode && isDesktop
                ? `calc(100% - ${sidebarWidth}px)`
                : "100%",
          }}
        >
          <Navbar
            dockMode={dockMode}
            onDrawerToggle={dockMode ? undefined : toggleMobileOpen}
          />

          {storageMode !== "home" && (
            <div
              style={{
                paddingLeft: pageInsetInline,
                paddingRight: pageInsetInline,
                paddingBottom: "var(--app-space-4)",
              }}
            >
              <AppAlert
                role="status"
                severity="warning"
                style={{ borderRadius: "var(--app-radius-card)" }}
              >
                <AppAlertTitle>
                  {storageMode === "memory"
                    ? "Settings are temporary"
                    : "Using fallback settings storage"}
                </AppAlertTitle>
                {storageMode === "memory"
                  ? "Persistent settings storage is unavailable. Changes will be lost when you refresh or sign out."
                  : "Home settings storage is unavailable. Settings are being saved in LinuxIO's persistent fallback storage."}
              </AppAlert>
            </div>
          )}

          {updateInfo?.available && (
            <div
              style={{
                paddingLeft: pageInsetInline,
                paddingRight: pageInsetInline,
                paddingTop: 0,
                paddingBottom: "var(--app-space-4)",
              }}
            >
              <UpdateBanner onDismiss={dismissUpdate} updateInfo={updateInfo} />
            </div>
          )}

          <div
            className="custom-scrollbar"
            style={{
              flex: 1,
              minHeight: 0,
              minWidth: 0,
              overflow: "auto",
              background: "var(--app-palette-background-default)",
              position: "relative",
            }}
          >
            {/*
             * Keep page spacing inside the scrollport. A sticky routed-tab
             * header is then pinned to the scrollport edge instead of the
             * scrollport's padded content edge, so page content cannot appear
             * in a gap above the header while it is stuck.
             */}
            <div
              data-app-route-content
              style={
                {
                  width: "100%",
                  height: "100%",
                  minHeight: "100%",
                  ...contentSpacing,
                  "--page-inset-block-start": pageInsetBlockStart,
                  "--page-inset-inline": pageInsetInline,
                } as CSSProperties & {
                  "--page-inset-block-start": string;
                  "--page-inset-inline": string;
                }
              }
            >
              <Outlet />
              <BootstrapLoaderReady />
            </div>
          </div>
        </div>
      </div>
      <div style={{ flexShrink: 0, width: "100%" }}>
        <Footer />
      </div>
    </div>
  );
};

export default MainLayout;
