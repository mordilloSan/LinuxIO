import { Box, CssBaseline } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { Suspense } from "react";
import React, { useEffect } from "react";
import { Outlet, useLocation } from "react-router-dom";

import ErrorBoundary from "@/components/errors/ErrorBoundary";
import Footer from "@/components/footer/Footer";
import PageLoader from "@/components/loaders/PageLoader";
import Navbar from "@/components/navbar/Navbar";
import Sidebar from "@/components/sidebar/Sidebar";
import dashboardItems from "@/components/sidebar/SidebarItems";
import { useConfigReady } from "@/hooks/useConfig";
import useSidebar from "@/hooks/useSidebar";
import PersistentFilebrowser from "@/pages/main/filebrowser/FilebrowserIframe";

const Dashboard: React.FC = () => {
  const location = useLocation();
  const theme = useTheme();
  const isLoaded = useConfigReady();
  const { toggleMobileOpen, setMobileOpen, sidebarWidth } = useSidebar();

  // Auto-close mobile drawer on route change
  useEffect(() => {
    setMobileOpen(false);
  }, [location.pathname, setMobileOpen]);

  // Wait for theme to load before rendering layout
  if (!isLoaded) return null;

  return (
    <Box sx={{ display: "flex", height: "100vh" }}>
      <CssBaseline />
      <Sidebar items={dashboardItems} />
      <Box
        sx={{
          flex: 1,
          display: "flex",
          flexDirection: "column",
          transition: theme.transitions.create(["margin-left", "width"], {
            easing: theme.transitions.easing.sharp,
            duration: theme.transitions.duration.leavingScreen,
          }),
          ml: { md: `${sidebarWidth}px` },
        }}
      >
        <Navbar onDrawerToggle={toggleMobileOpen} />
        <Box
          className="custom-scrollbar"
          sx={{
            flex: 1,
            overflow: "auto",
            background: theme.palette.background.default,
            p: { xs: 5, lg: 7 },
            position: "relative",
          }}
        >
          {/* Persistent iframe sits above the Outlet *inside* content area */}
          <PersistentFilebrowser />
          <ErrorBoundary>
            <Suspense fallback={<PageLoader />}>
              <Outlet />
            </Suspense>
          </ErrorBoundary>
        </Box>
        <Footer />
      </Box>
    </Box>
  );
};

export default Dashboard;
