import type { CSSProperties, ReactNode } from "react";

import { linuxio } from "@/api";
import type { VirtualMachine } from "@/api";
import AppDivider from "@/components/ui/AppDivider";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow, { DetailRow } from "@/components/ui/InfoRow";
import { formatThroughput } from "@/utils/formatters";

import { VMMetricBars } from "./VMCard";
import { VM_TOAST, formatDisk } from "./vmShared";

const sectionStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-4)",
  minWidth: 0,
};

const formatRate = (bytesPerSec: number | undefined) =>
  bytesPerSec === undefined ? "Unavailable" : formatThroughput(bytesPerSec);

const SectionTitle = ({ children }: { children: ReactNode }) => (
  <AppTypography fontWeight={700} style={{ margin: 0 }} variant="subtitle2">
    {children}
  </AppTypography>
);

const valueText = (text: string) => (
  <AppTypography
    component="span"
    fontWeight={500}
    noWrap
    title={text}
    variant="caption"
  >
    {text}
  </AppTypography>
);

const emptyText = (text: string) => (
  <AppTypography color="text.secondary" variant="body2">
    {text}
  </AppTypography>
);

/**
 * The focused VM's monitoring, settings, disks and network, laid out like a
 * container's details beside its card.
 *
 * Loading and failure are owned by the `/vm/machines/$name` route: its loader
 * suspends on `virt.get` and its errors surface at the route boundary, which
 * renders here in the parent layout's outlet.
 */
export default function VMDetailsPanel({ vm }: { vm: VirtualMachine }) {
  const autostartMutation = linuxio.virt.set_autostart.useMutation({
    success: "Autostart updated",
    error: "Failed to change autostart",
    toast: VM_TOAST,
  });
  const disks = vm.disks ?? [];
  const nics = vm.nics ?? [];

  return (
    <div style={sectionStyle}>
      <div style={sectionStyle}>
        <SectionTitle>Monitoring</SectionTitle>
        <VMMetricBars vm={vm} />
        <AppDivider style={{ marginBlock: "var(--app-space-4)" }} />
        <InfoRow label="Net Receive rate">
          {formatRate(vm.metrics?.networkReceiveBytesPerSecond)}
        </InfoRow>
        <InfoRow label="Net Send rate">
          {formatRate(vm.metrics?.networkSendBytesPerSecond)}
        </InfoRow>
        <InfoRow label="Disk Read rate">
          {formatRate(vm.metrics?.diskReadBytesPerSecond)}
        </InfoRow>
        <InfoRow label="Disk Write rate" noBorder>
          {formatRate(vm.metrics?.diskWriteBytesPerSecond)}
        </InfoRow>
      </div>
      <AppDivider style={{ marginBlock: "var(--app-space-4)" }} />
      <div style={sectionStyle}>
        <SectionTitle>Settings</SectionTitle>
        <DetailRow label="Autostart" noBorder>
          <AppSwitch
            aria-label="Autostart"
            checked={vm.autostart}
            disabled={autostartMutation.isPending}
            onChange={(_, autostart) =>
              autostartMutation.mutate({ autostart, name: vm.name })
            }
            size="small"
          />
        </DetailRow>
      </div>
      <AppDivider style={{ marginBlock: "var(--app-space-4)" }} />
      <div style={sectionStyle}>
        <SectionTitle>Disks</SectionTitle>
        {disks.length === 0
          ? emptyText("No disks reported.")
          : disks.map((disk, index) => (
              <DetailRow
                key={`${disk.target}-${disk.path}`}
                label={disk.target || disk.device}
                noBorder={index === 0}
              >
                {valueText(
                  [
                    disk.owned ? "managed" : "external",
                    disk.sizeGB ? formatDisk(disk.sizeGB) : "",
                    disk.path,
                  ]
                    .filter(Boolean)
                    .join(" · "),
                )}
              </DetailRow>
            ))}
      </div>
      <AppDivider style={{ marginBlock: "var(--app-space-4)" }} />
      <div style={sectionStyle}>
        <SectionTitle>Networks</SectionTitle>
        {nics.length === 0
          ? emptyText("No interfaces reported.")
          : nics.map((nic, index) => (
              <DetailRow
                key={`${nic.mac}-${index}`}
                label={nic.network || "network"}
                noBorder={index === 0}
              >
                {valueText(
                  [
                    (nic.ipAddresses ?? []).join(", ") || "No IP lease yet",
                    nic.mac || nic.model || "virtio",
                  ].join(" · "),
                )}
              </DetailRow>
            ))}
      </div>
    </div>
  );
}
