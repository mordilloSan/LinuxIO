import { Icon } from "@iconify/react";
import React, { useState } from "react";

import NFSShares from "./NFSShares";
import SambaShares from "./SambaShares";

import { TabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

const SharesPage: React.FC = () => {
  const [createNFSHandler, setCreateNFSHandler] = useState<(() => void) | null>(
    null,
  );
  const [createSambaHandler, setCreateSambaHandler] = useState<
    (() => void) | null
  >(null);
  const [nfsView, setNfsView] = useViewMode("shares.nfs", "table");
  const [sambaView, setSambaView] = useViewMode("shares.samba", "table");

  return (
    <TabContainer
      tabs={[
        {
          value: "nfs",
          label: "NFS Exports",
          component: (
            <NFSShares
              onCreateHandler={(handler) => setCreateNFSHandler(() => handler)}
              viewMode={nfsView}
            />
          ),
          rightContent: (
            <>
              <AppTooltip
                title={
                  nfsView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <AppIconButton
                  size="small"
                  onClick={() =>
                    setNfsView(nfsView === "table" ? "card" : "table")
                  }
                >
                  {nfsView === "table" ? (
                    <Icon icon="mdi:view-grid" width={20} height={20} />
                  ) : (
                    <Icon icon="mdi:table-row" width={20} height={20} />
                  )}
                </AppIconButton>
              </AppTooltip>
              {createNFSHandler && (
                <AppButton
                  variant="contained"
                  size="small"
                  onClick={createNFSHandler}
                  startIcon={<Icon icon="mdi:plus" width={20} height={20} />}
                >
                  Add Export
                </AppButton>
              )}
            </>
          ),
        },
        {
          value: "samba",
          label: "Samba Shares",
          component: (
            <SambaShares
              onCreateHandler={(handler) =>
                setCreateSambaHandler(() => handler)
              }
              viewMode={sambaView}
            />
          ),
          rightContent: (
            <>
              <AppTooltip
                title={
                  sambaView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <AppIconButton
                  size="small"
                  onClick={() =>
                    setSambaView(sambaView === "table" ? "card" : "table")
                  }
                >
                  {sambaView === "table" ? (
                    <Icon icon="mdi:view-grid" width={20} height={20} />
                  ) : (
                    <Icon icon="mdi:table-row" width={20} height={20} />
                  )}
                </AppIconButton>
              </AppTooltip>
              {createSambaHandler && (
                <AppButton
                  variant="contained"
                  size="small"
                  onClick={createSambaHandler}
                  startIcon={<Icon icon="mdi:plus" width={20} height={20} />}
                >
                  Add Share
                </AppButton>
              )}
            </>
          ),
        },
      ]}
      defaultTab="nfs"
      urlParam="sharesTab"
    />
  );
};

export default SharesPage;
