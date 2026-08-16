import { useCanGoBack, useNavigate, useRouter } from "@tanstack/react-router";
import { useCallback } from "react";

import SettingsDialog from "@/routes/_authenticated/-components/navbar/SettingsDialog";

/* Interim shape of the route: the page surface is still empty, so settings
   keep their dialog and float it over the page background. The sections move
   onto the page itself once they are laid out for a full-width surface. */
const SettingsPage = () => {
  const router = useRouter();
  const canGoBack = useCanGoBack();
  const navigate = useNavigate();

  /* Closing leaves the route rather than baring the empty page. Entering
     settings by URL has nothing to go back to, so that lands on the
     dashboard. */
  const handleClose = useCallback(() => {
    if (canGoBack) {
      router.history.back();
      return;
    }

    void navigate({ to: "/" });
  }, [canGoBack, navigate, router]);

  return <SettingsDialog onClose={handleClose} open />;
};

export default SettingsPage;
