import { Icon } from "@iconify/react";

import { linuxio } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

export function useDockerUpdateCheck() {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const { isEnabled: watchtowerEnabled, reason: watchtowerReason } =
    useCapability("watchtowerAvailable");
  const { mutate: checkUpdates, isPending: isCheckingUpdates } =
    linuxio.docker.check_updates.useJobAction({
      success: (result) => {
        const checked = result?.checked ?? 0;
        const updates = result?.updates ?? 0;
        toast.success(
          `Checked ${checked} container(s), found ${updates} update(s)`,
        );
      },
      error: "Failed to check updates",
      toast: DOCKER_TOAST_META,
    });

  const button = (
    <AppButton
      disabled={isCheckingUpdates || !watchtowerEnabled}
      onClick={() => checkUpdates()}
      size="small"
      startIcon={
        isCheckingUpdates ? (
          <AppCircularProgress color="inherit" size={18} />
        ) : (
          <Icon height={20} icon="mdi:update" width={20} />
        )
      }
      variant="outlined"
    >
      Check Updates
    </AppButton>
  );

  return {
    button: watchtowerEnabled ? (
      button
    ) : (
      <AppTooltip title={watchtowerReason}>
        <span>{button}</span>
      </AppTooltip>
    ),
    isCheckingUpdates,
  };
}
