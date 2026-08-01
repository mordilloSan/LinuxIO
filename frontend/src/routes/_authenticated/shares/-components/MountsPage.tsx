import { Icon } from "@iconify/react";
import { useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useViewMode } from "@/hooks/useViewMode";

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
  const [nfsView, setNfsView] = useViewMode("shares.mounts", "table");

  const actions = (
    <>
      <AppTooltip
        title={
          nfsView === "table" ? "Switch to card view" : "Switch to table view"
        }
      >
        <AppIconButton
          aria-label={
            nfsView === "table" ? "Switch to card view" : "Switch to table view"
          }
          onClick={() => setNfsView(nfsView === "table" ? "card" : "table")}
          size="small"
        >
          {nfsView === "table" ? (
            <Icon height={20} icon="mdi:card-multiple" width={20} />
          ) : (
            <Icon height={20} icon="mdi:table" width={20} />
          )}
        </AppIconButton>
      </AppTooltip>
      {mountNFSHandler && (
        <AppTooltip title={nfsUnavailable ? nfsReason : "Mount NFS"}>
          <span>
            <AppButton
              disabled={nfsUnavailable}
              onClick={mountNFSHandler}
              size="small"
              startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
              variant="contained"
            >
              Mount NFS
            </AppButton>
          </span>
        </AppTooltip>
      )}
      {mountSMBHandler && (
        <AppTooltip
          title={sambaClientUnavailable ? sambaClientReason : "Mount SMB"}
        >
          <span>
            <AppButton
              disabled={sambaClientUnavailable}
              onClick={mountSMBHandler}
              size="small"
              startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
              variant="contained"
            >
              Mount SMB
            </AppButton>
          </span>
        </AppTooltip>
      )}
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 24,
          height: "100%",
          minHeight: 0,
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
            viewMode={nfsView}
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
          />
        </div>
      </div>
    </>
  );
};

export default MountsPage;
