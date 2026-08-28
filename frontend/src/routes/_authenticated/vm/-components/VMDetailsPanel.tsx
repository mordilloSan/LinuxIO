import type { CSSProperties } from "react";

import type { VirtualMachine } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppChip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppMediaQuery } from "@/theme";
import { down } from "@/theme/breakpoints";

import {
  formatDisk,
  formatMemory,
  normalizeState,
  stateChipColor,
} from "./vmShared";

const detailPanelStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-16)",
  minWidth: 0,
  padding: "var(--app-space-16)",
};

const detailPanelEmptyStyle: CSSProperties = {
  ...detailPanelStyle,
  alignItems: "center",
  justifyContent: "center",
  minHeight: 220,
};

const detailHeaderStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  gap: "var(--app-space-16)",
  justifyContent: "space-between",
};

const statGridStyle = (isMobile: boolean): CSSProperties => ({
  display: "grid",
  gap: "var(--app-space-8)",
  gridTemplateColumns: isMobile ? "1fr" : "repeat(2, minmax(0, 1fr))",
});

const statItemStyle: CSSProperties = {
  border: "1px solid var(--app-palette-divider)",
  borderRadius: 6,
  padding: "var(--app-space-8)",
};

const detailSectionStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-8)",
};

const tokenListStyle: CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  gap: "var(--app-space-8)",
};

const tokenStyle: CSSProperties = {
  border: "1px solid var(--app-palette-divider)",
  borderRadius: 6,
  display: "flex",
  flexDirection: "column",
  gap: 2,
  maxWidth: "100%",
  minWidth: 110,
  padding: "var(--app-space-8)",
};

const wrappingCodeStyle: CSSProperties = {
  overflowWrap: "anywhere",
};

/**
 * Renders one VM's detail, or the empty prompt when no machine is selected.
 *
 * Loading and failure are owned by the `/vm/machines/$name` route: its loader
 * suspends on `virt.get` and its errors surface at the route boundary, which
 * renders here in the parent layout's outlet.
 */
export default function VMDetailsPanel({ vm }: { vm: VirtualMachine | null }) {
  const isMobile = useAppMediaQuery(down("sm"));

  if (!vm) {
    return (
      <FrostedCard style={detailPanelEmptyStyle}>
        <AppTypography color="text.secondary" variant="body2">
          Select a VM
        </AppTypography>
      </FrostedCard>
    );
  }

  return (
    <FrostedCard style={detailPanelStyle}>
      <div style={detailHeaderStyle}>
        <div style={{ minWidth: 0 }}>
          <AppTypography component="h2" variant="h6">
            {vm.name}
          </AppTypography>
          <AppTypography color="text.secondary" noWrap variant="caption">
            {vm.uuid || "No UUID"}
          </AppTypography>
        </div>
        <AppChip
          color={stateChipColor(vm.state)}
          label={normalizeState(vm.state)}
          size="small"
          variant="soft"
        />
      </div>
      <div style={statGridStyle(isMobile)}>
        <div style={statItemStyle}>
          <AppTypography
            color="text.secondary"
            component="span"
            variant="caption"
          >
            vCPUs
          </AppTypography>
          <strong style={{ display: "block", marginTop: 4 }}>{vm.vcpus}</strong>
        </div>
        <div style={statItemStyle}>
          <AppTypography
            color="text.secondary"
            component="span"
            variant="caption"
          >
            Memory
          </AppTypography>
          <strong style={{ display: "block", marginTop: 4 }}>
            {formatMemory(vm.memoryMB)}
          </strong>
        </div>
        <div style={statItemStyle}>
          <AppTypography
            color="text.secondary"
            component="span"
            variant="caption"
          >
            Disk
          </AppTypography>
          <strong style={{ display: "block", marginTop: 4 }}>
            {formatDisk(vm.diskGB)}
          </strong>
        </div>
        <div style={statItemStyle}>
          <AppTypography
            color="text.secondary"
            component="span"
            variant="caption"
          >
            Autostart
          </AppTypography>
          <strong style={{ display: "block", marginTop: 4 }}>
            {vm.autostart ? "On" : "Off"}
          </strong>
        </div>
      </div>
      <div style={detailSectionStyle}>
        <AppTypography component="h3" variant="subtitle2">
          Disks
        </AppTypography>
        {(vm.disks ?? []).length === 0 ? (
          <AppTypography color="text.secondary" variant="body2">
            No disks reported.
          </AppTypography>
        ) : (
          <div style={tokenListStyle}>
            {(vm.disks ?? []).map((disk) => (
              <span key={`${disk.target}-${disk.path}`} style={tokenStyle}>
                <code style={wrappingCodeStyle}>
                  {disk.target || disk.device}
                </code>
                <AppTypography
                  color="text.secondary"
                  component="small"
                  variant="caption"
                >
                  {disk.owned ? "managed" : "external"}
                </AppTypography>
              </span>
            ))}
          </div>
        )}
      </div>
      <div style={detailSectionStyle}>
        <AppTypography component="h3" variant="subtitle2">
          Network
        </AppTypography>
        {(vm.nics ?? []).length === 0 ? (
          <AppTypography color="text.secondary" variant="body2">
            No interfaces reported.
          </AppTypography>
        ) : (
          <div style={tokenListStyle}>
            {(vm.nics ?? []).map((nic, index) => (
              <span key={`${nic.mac}-${index}`} style={tokenStyle}>
                <code style={wrappingCodeStyle}>
                  {nic.network || "network"}
                </code>
                {(nic.ipAddresses ?? []).length === 0 ? (
                  <AppTypography
                    color="text.secondary"
                    component="small"
                    variant="caption"
                  >
                    No IP lease yet
                  </AppTypography>
                ) : (
                  (nic.ipAddresses ?? []).map((ip) => (
                    <AppTypography
                      color="text.secondary"
                      component="small"
                      key={ip}
                      variant="caption"
                    >
                      {ip}
                    </AppTypography>
                  ))
                )}
                <AppTypography
                  color="text.secondary"
                  component="small"
                  variant="caption"
                >
                  {nic.mac || nic.model || "virtio"}
                </AppTypography>
              </span>
            ))}
          </div>
        )}
      </div>
    </FrostedCard>
  );
}
