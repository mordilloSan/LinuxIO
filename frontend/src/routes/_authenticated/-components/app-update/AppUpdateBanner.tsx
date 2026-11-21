import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { CACHE_TTL_MS, linuxio } from "@/api";
import AppAlert from "@/components/ui/AppAlert";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import useAuth from "@/hooks/useAuth";

/**
 * Names the LinuxIO update PackageKit lists for the linuxio package, to a
 * session that can install it, and opens the Updates page, where it is
 * installed like any other package. Closing it hides that version until the
 * page is loaded again.
 */
const AppUpdateBanner = () => {
  const { privileged } = useAuth();
  const { data } = useQuery({
    ...linuxio.control.version,
    enabled: privileged,
    retry: false,
    staleTime: CACHE_TTL_MS.FIVE_MINUTES,
  });
  const [dismissed, setDismissed] = useState<string | null>(null);
  const version = data?.update_version;

  if (!privileged || !version || version === dismissed) return null;

  return (
    <AppAlert
      action={
        <AppRouterLinkButton
          size="small"
          style={{ whiteSpace: "nowrap" }}
          to="/updates"
          variant="contained"
        >
          Open Updates
        </AppRouterLinkButton>
      }
      className="app-alert--centered"
      onClose={() => setDismissed(version)}
      role="status"
      severity="info"
      style={{
        alignItems: "center",
        backgroundColor: "var(--update-banner-bg)",
        borderRadius: "var(--app-radius-card)",
        color: "var(--update-banner-color)",
        marginBottom: "var(--app-space-4)",
      }}
    >
      LinuxIO {version} is available.
    </AppAlert>
  );
};

export default AppUpdateBanner;
