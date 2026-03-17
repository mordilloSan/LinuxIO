import { CssBaseline, useMediaQuery } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { Suspense, useEffect } from "react";
import { Outlet, useLocation } from "react-router-dom";

import ErrorBoundary from "@/components/errors/ErrorBoundary";
import Footer from "@/components/footer/Footer";
import PageLoader from "@/components/loaders/PageLoader";
import Navbar from "@/components/navbar/Navbar";
import Sidebar from "@/components/sidebar/Sidebar";
import UpdateBanner from "@/components/update/UpdateBanner";
import { useConfigReady } from "@/hooks/useConfig";
import useSidebar from "@/hooks/useSidebar";
import { useUpdateInfo } from "@/hooks/useUpdateInfo";
import { useSidebarItems } from "@/routes";

const Dashboard: React.FC = () => {
  const location = useLocation();
  const theme = useTheme();
  const isSmallUp = useMediaQuery(theme.breakpoints.up("sm"));
  const isLoaded = useConfigReady();
  const { toggleMobileOpen, setMobileOpen, sidebarWidth, isDesktop } =
    useSidebar();
  const { updateInfo, dismissUpdate } = useUpdateInfo();
  const sidebarItems = useSidebarItems();

  useEffect(() => {
    if (!isDesktop) setMobileOpen(false);
  }, [location.key, isDesktop, setMobileOpen]);

  if (!isLoaded) return null;

  const contentSpacing =
    location.pathname === "/"
      ? {
          paddingLeft: isSmallUp ? theme.spacing(5) : theme.spacing(4),
          paddingRight: isSmallUp ? theme.spacing(7) : theme.spacing(2),
          paddingTop: 0,
          paddingBottom: 0,
        }
      : location.pathname.includes("/filebrowser")
        ? { padding: 0 }
        : {
            paddingLeft: isSmallUp ? theme.spacing(5) : theme.spacing(2),
            paddingRight: isSmallUp ? theme.spacing(7) : theme.spacing(2),
            paddingTop: theme.spacing(5),
            paddingBottom: theme.spacing(5),
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
      <CssBaseline />
      <div
        style={{
          display: "flex",
          flex: 1,
          minHeight: 0,
          minWidth: 0,
          overflow: "hidden",
        }}
      >
        <Sidebar items={sidebarItems} />
        <div
          style={{
            flex: 1,
            display: "flex",
            flexDirection: "column",
            minHeight: 0,
            minWidth: 0,
            transition: theme.transitions.create(["margin-left", "width"], {
              easing: theme.transitions.easing.sharp,
              duration: theme.transitions.duration.leavingScreen,
            }),
            marginLeft: isDesktop ? `${sidebarWidth}px` : undefined,
            width: isDesktop ? `calc(100% - ${sidebarWidth}px)` : "100%",
          }}
        >
          <Navbar onDrawerToggle={toggleMobileOpen} />

          {updateInfo?.available && (
            <UpdateBanner updateInfo={updateInfo} onDismiss={dismissUpdate} />
          )}

          <div
            className="custom-scrollbar"
            style={{
              flex: 1,
              minHeight: 0,
              minWidth: 0,
              overflow: "auto",
              background: theme.palette.background.default,
              position: "relative",
              ...contentSpacing,
            }}
          >
            <ErrorBoundary>
              <Suspense fallback={<PageLoader />}>
                <Outlet />
              </Suspense>
            </ErrorBoundary>
          </div>
        </div>
      </div>
      <div style={{ flexShrink: 0, width: "100%" }}>
        <Footer />
      </div>
    </div>
  );
};

export default Dashboard;
