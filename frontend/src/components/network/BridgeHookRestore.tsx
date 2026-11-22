import { linuxio, type NetworkBridgeHook } from "@/api";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import useAuth from "@/hooks/useAuth";

interface BridgeHookRestoreProps {
  hook?: NetworkBridgeHook;
}

/**
 * Offers to put back the LinuxIO libvirt hook that a warning above reports as
 * missing or not executable. A success refetches the bridge options and the
 * VM preflight through the route's invalidations, and the offer goes with the
 * warning. The parent keeps this mounted while the warning goes, so a failure
 * stays in view.
 */
const BridgeHookRestore = ({ hook }: BridgeHookRestoreProps) => {
  const { privileged } = useAuth();
  const {
    error,
    isPending,
    mutate: restore,
  } = linuxio.network.restore_bridge_hook.useMutation({
    success: "LinuxIO libvirt hook restored",
  });

  const offered =
    privileged && !!hook && hook.state !== "installed" && hook.restorable;
  if (!offered && !error) return null;

  return (
    <>
      {offered && (
        <AppAlert
          action={
            <AppButton
              disabled={isPending}
              onClick={() => restore()}
              size="small"
              variant="outlined"
            >
              {isPending ? "Restoring…" : "Restore"}
            </AppButton>
          }
          severity="info"
        >
          Restore puts back the LinuxIO libvirt hook from LinuxIO&apos;s own
          copy and restarts the libvirt daemon so it runs the hook. Running VMs
          keep running.
        </AppAlert>
      )}
      {error && (
        <AppAlert severity="error">
          <AppAlertTitle>Unable to restore the libvirt hook</AppAlertTitle>
          {error.message}
        </AppAlert>
      )}
    </>
  );
};

export default BridgeHookRestore;
