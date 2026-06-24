import type { CSSProperties } from "react";

import {
  formatDisk,
  formatMemory,
  normalizeState,
  stateChipColor,
} from "./vmShared";

import type { VirtualMachine } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppChip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { type AppTheme, useAppMediaQuery, useAppTheme } from "@/theme";

const detailPanelStyle = (theme: AppTheme): CSSProperties => ({
  display: "flex",
  flexDirection: "column",
  gap: theme.spacing(4),
  minWidth: 0,
  padding: theme.spacing(4),
});

const detailPanelEmptyStyle = (theme: AppTheme): CSSProperties => ({
  ...detailPanelStyle(theme),
  alignItems: "center",
  justifyContent: "center",
  minHeight: 220,
});

const detailHeaderStyle = (theme: AppTheme): CSSProperties => ({
  alignItems: "center",
  display: "flex",
  gap: theme.spacing(4),
  justifyContent: "space-between",
});

const statGridStyle = (theme: AppTheme, isMobile: boolean): CSSProperties => ({
  display: "grid",
  gap: theme.spacing(2.5),
  gridTemplateColumns: isMobile ? "1fr" : "repeat(2, minmax(0, 1fr))",
});

const statItemStyle = (theme: AppTheme): CSSProperties => ({
  border: `1px solid ${theme.palette.divider}`,
  borderRadius: 6,
  padding: theme.spacing(2.5),
});

const statLabelStyle = (theme: AppTheme): CSSProperties => ({
  color: theme.palette.text.secondary,
  display: "block",
  fontSize: "0.75rem",
});

const detailSectionStyle = (theme: AppTheme): CSSProperties => ({
  display: "flex",
  flexDirection: "column",
  gap: theme.spacing(2),
});

const tokenListStyle = (theme: AppTheme): CSSProperties => ({
  display: "flex",
  flexWrap: "wrap",
  gap: theme.spacing(2),
});

const tokenStyle = (theme: AppTheme): CSSProperties => ({
  border: `1px solid ${theme.palette.divider}`,
  borderRadius: 6,
  display: "flex",
  flexDirection: "column",
  gap: 2,
  maxWidth: "100%",
  minWidth: 110,
  padding: theme.spacing(2),
});

const wrappingCodeStyle: CSSProperties = {
  overflowWrap: "anywhere",
};

export default function VMDetailsPanel({ vm }: { vm: VirtualMachine | null }) {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));

  if (!vm) {
    return (
      <FrostedCard style={detailPanelEmptyStyle(theme)}>
        <AppTypography color="text.secondary" variant="body2">
          Select a VM
        </AppTypography>
      </FrostedCard>
    );
  }

  return (
    <FrostedCard style={detailPanelStyle(theme)}>
      <div style={detailHeaderStyle(theme)}>
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
      <div style={statGridStyle(theme, isMobile)}>
        <div style={statItemStyle(theme)}>
          <span style={statLabelStyle(theme)}>vCPUs</span>
          <strong style={{ display: "block", marginTop: 4 }}>{vm.vcpus}</strong>
        </div>
        <div style={statItemStyle(theme)}>
          <span style={statLabelStyle(theme)}>Memory</span>
          <strong style={{ display: "block", marginTop: 4 }}>
            {formatMemory(vm.memoryMB)}
          </strong>
        </div>
        <div style={statItemStyle(theme)}>
          <span style={statLabelStyle(theme)}>Disk</span>
          <strong style={{ display: "block", marginTop: 4 }}>
            {formatDisk(vm.diskGB)}
          </strong>
        </div>
        <div style={statItemStyle(theme)}>
          <span style={statLabelStyle(theme)}>Autostart</span>
          <strong style={{ display: "block", marginTop: 4 }}>
            {vm.autostart ? "On" : "Off"}
          </strong>
        </div>
      </div>
      <div style={detailSectionStyle(theme)}>
        <AppTypography component="h3" variant="subtitle2">
          Disks
        </AppTypography>
        {(vm.disks ?? []).length === 0 ? (
          <AppTypography color="text.secondary" variant="body2">
            No disks reported.
          </AppTypography>
        ) : (
          <div style={tokenListStyle(theme)}>
            {(vm.disks ?? []).map((disk) => (
              <span
                key={`${disk.target}-${disk.path}`}
                style={tokenStyle(theme)}
              >
                <code style={wrappingCodeStyle}>
                  {disk.target || disk.device}
                </code>
                <small style={statLabelStyle(theme)}>
                  {disk.owned ? "managed" : "external"}
                </small>
              </span>
            ))}
          </div>
        )}
      </div>
      <div style={detailSectionStyle(theme)}>
        <AppTypography component="h3" variant="subtitle2">
          Network
        </AppTypography>
        {(vm.nics ?? []).length === 0 ? (
          <AppTypography color="text.secondary" variant="body2">
            No interfaces reported.
          </AppTypography>
        ) : (
          <div style={tokenListStyle(theme)}>
            {(vm.nics ?? []).map((nic, index) => (
              <span key={`${nic.mac}-${index}`} style={tokenStyle(theme)}>
                <code style={wrappingCodeStyle}>
                  {nic.network || "network"}
                </code>
                {(nic.ipAddresses ?? []).length === 0 ? (
                  <small style={statLabelStyle(theme)}>No IP lease yet</small>
                ) : (
                  (nic.ipAddresses ?? []).map((ip) => (
                    <small key={ip} style={statLabelStyle(theme)}>
                      {ip}
                    </small>
                  ))
                )}
                <small style={statLabelStyle(theme)}>
                  {nic.mac || nic.model || "virtio"}
                </small>
              </span>
            ))}
          </div>
        )}
      </div>
    </FrostedCard>
  );
}
