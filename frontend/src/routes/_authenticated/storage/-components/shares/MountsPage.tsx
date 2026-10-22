import { useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppTypography from "@/components/ui/AppTypography";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useCapability } from "@/hooks/useCapabilities";
import { useViewMode } from "@/hooks/useViewMode";
import { GAP_XL } from "@/theme/constants";

import CIFSMounts from "./CIFSMounts";
import NFSMounts from "./NFSMounts";

const MountsPage = () => {
  const { reason: nfsReason, status: nfsStatus } =
    useCapability("nfsClientAvailable");
  const nfsUnavailable = nfsStatus === "unavailable";
  const { reason: sambaClientReason, status: sambaClientStatus } =
    useCapability("sambaClientAvailable");
  const sambaClientUnavailable = sambaClientStatus === "unavailable";
  const [mountNFSHandler, setMountNFSHandler] = useState<(() => void) | null>(
    null,
  );
  const [mountSMBHandler, setMountSMBHandler] = useState<(() => void) | null>(
    null,
  );
  const [mountsView, setMountsView] = useViewMode("shares.mounts");

  const actions = (
    <HeaderActions
      create={
        <>
          {mountNFSHandler && (
            <AppActionIconButton
              ariaLabel="Mount NFS"
              disabled={nfsUnavailable}
              icon="mdi:plus-network-outline"
              iconSize={20}
              label={nfsUnavailable ? nfsReason : "Mount NFS"}
              onClick={mountNFSHandler}
            />
          )}
          {mountSMBHandler && (
            <AppActionIconButton
              ariaLabel="Mount SMB"
              disabled={sambaClientUnavailable}
              icon="mdi:plus-box-outline"
              iconSize={20}
              label={sambaClientUnavailable ? sambaClientReason : "Mount SMB"}
              onClick={mountSMBHandler}
            />
          )}
        </>
      }
      view={
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setMountsView}
          viewMode={mountsView}
        />
      }
    />
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: GAP_XL,
          height: "100%",
          minHeight: 0,
          minWidth: 0,
          overflow: "auto",
        }}
      >
        <div>
          <AppTypography fontWeight={600} gutterBottom variant="subtitle1">
            NFS
          </AppTypography>
          <NFSMounts
            onMountCreateHandler={(handler) =>
              setMountNFSHandler(() => handler)
            }
            viewMode={mountsView}
          />
        </div>
        <div>
          <AppTypography fontWeight={600} gutterBottom variant="subtitle1">
            SMB / CIFS
          </AppTypography>
          <CIFSMounts
            onMountCreateHandler={(handler) =>
              setMountSMBHandler(() => handler)
            }
            viewMode={mountsView}
          />
        </div>
      </div>
    </>
  );
};

export default MountsPage;
