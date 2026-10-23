import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import type { VirtualMachine } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import MetricBar from "@/components/gauge/MetricBar";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { DetailRow } from "@/components/ui/InfoRow";
import StatusDot from "@/components/ui/StatusDot";
import { getVMStatusColor } from "@/constants/statusColors";
import { CARD_PADDING_LG } from "@/theme/constants";

import { ACTION_PROGRESS_LABEL } from "./useVMLifecycle";
import {
  type VMAction,
  formatDisk,
  formatMemory,
  normalizeState,
  vmIPAddresses,
  vmOSIcon,
  vmOSLabel,
} from "./vmShared";

const valueText = (text: string) => (
  <AppTypography
    component="div"
    fontWeight={500}
    noWrap
    title={text}
    variant="caption"
  >
    {text}
  </AppTypography>
);

/** CPU and memory bars, shaped like a container card's. */
export function VMMetricBars({ vm }: { vm: VirtualMachine }) {
  const { metrics } = vm;
  const cpuPercent = metrics?.cpuPercent;
  return (
    <>
      <MetricBar
        color="var(--app-palette-primary-main)"
        label="CPU"
        percent={cpuPercent ?? 0}
        rightLabel={
          cpuPercent === undefined ? "Unavailable" : `${cpuPercent.toFixed(1)}%`
        }
        tooltip="CPU Usage"
      />
      <MetricBar
        color="var(--app-palette-primary-main)"
        label="MEM"
        percent={
          metrics && vm.memoryMB
            ? Math.min((metrics.memoryUsedMB / vm.memoryMB) * 100, 100)
            : 0
        }
        rightLabel={
          metrics
            ? `${formatMemory(metrics.memoryUsedMB)} / ${formatMemory(vm.memoryMB)}`
            : "Unavailable"
        }
        tooltip="Memory Usage"
      />
    </>
  );
}

/**
 * A VM card. In the grid it mirrors a container card: logo, name (which opens
 * the details), the action strip and usage bars. Selected, it becomes the
 * focused summary beside the details panel.
 */
export default function VMCard({
  actions,
  onSelect,
  pendingAction,
  selected = false,
  vm,
}: {
  actions: ReactNode;
  onSelect: () => void;
  pendingAction?: VMAction;
  selected?: boolean;
  vm: VirtualMachine;
}) {
  const stateLabel = pendingAction
    ? ACTION_PROGRESS_LABEL[pendingAction]
    : normalizeState(vm.state);
  const statusDot = (
    <StatusDot color={getVMStatusColor(vm.state)} tooltip={stateLabel} />
  );
  const addresses = vmIPAddresses(vm);

  return (
    <FrostedCard
      accent
      hoverLift={!selected}
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minWidth: 0,
        padding: CARD_PADDING_LG,
        width: "100%",
        ...(selected && { borderBottomColor: "transparent" }),
      }}
    >
      {selected ? (
        <>
          <AppButton
            aria-label={`Collapse ${vm.name}`}
            color="inherit"
            fullWidth
            onClick={onSelect}
            style={{
              alignItems: "flex-start",
              color: "inherit",
              gap: 8,
              justifyContent: "space-between",
              marginBottom: 12,
              padding: 0,
              textAlign: "left",
            }}
          >
            <div
              style={{
                alignItems: "center",
                display: "flex",
                flex: 1,
                gap: 8,
                minWidth: 0,
              }}
            >
              <Icon
                color="var(--app-palette-primary-main)"
                height={36}
                icon={vmOSIcon(vm.os)}
                width={36}
              />
              <div style={{ flex: 1, minWidth: 0 }}>
                <AppTypography
                  component="div"
                  fontWeight="bold"
                  noWrap
                  title={vm.name}
                  variant="body2"
                >
                  {vm.name}
                </AppTypography>
                <AppTypography
                  color="text.secondary"
                  component="div"
                  noWrap
                  style={{ marginTop: 2 }}
                  variant="caption"
                >
                  {`${vm.vcpus} vCPU · ${formatMemory(vm.memoryMB)} · ${formatDisk(vm.diskGB)}`}
                </AppTypography>
              </div>
            </div>
            <span style={{ marginTop: 4 }}>{statusDot}</span>
          </AppButton>
          <div style={{ flex: 1, minWidth: 0 }}>
            <AppTypography
              fontWeight={700}
              style={{ margin: 0 }}
              variant="subtitle2"
            >
              Overview
            </AppTypography>
            <DetailRow label="State" noBorder>
              {valueText(stateLabel)}
            </DetailRow>
            <DetailRow label="OS">{valueText(vmOSLabel(vm.os))}</DetailRow>
            <DetailRow label="Firmware">
              {valueText(vm.firmware === "uefi" ? "UEFI" : "BIOS (legacy)")}
            </DetailRow>
            <DetailRow label="IP">
              {valueText(addresses.length > 0 ? addresses.join(", ") : "—")}
            </DetailRow>
            <DetailRow label="UUID">{valueText(vm.uuid || "—")}</DetailRow>
          </div>
          <div style={{ marginTop: 12 }}>{actions}</div>
        </>
      ) : (
        <>
          <div style={{ alignItems: "center", display: "flex", width: "100%" }}>
            <Icon
              color="var(--app-palette-primary-main)"
              height={48}
              icon={vmOSIcon(vm.os)}
              style={{ alignSelf: "flex-start", flexShrink: 0, marginRight: 6 }}
              width={48}
            />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ alignItems: "center", display: "flex", gap: 4 }}>
                <AppButton
                  aria-label={`Select ${vm.name}`}
                  color="inherit"
                  fullWidth
                  onClick={onSelect}
                  style={{
                    color: "inherit",
                    justifyContent: "flex-start",
                    minWidth: 0,
                    padding: 0,
                    textAlign: "left",
                  }}
                >
                  <AppTypography
                    fontWeight={600}
                    noWrap
                    style={{ marginBottom: 2, marginLeft: 4 }}
                    title={vm.name}
                    variant="h5"
                  >
                    {vm.name}
                  </AppTypography>
                </AppButton>
                {statusDot}
              </div>
              {actions}
            </div>
          </div>
          <div style={{ marginTop: 8, width: "100%" }}>
            <VMMetricBars vm={vm} />
          </div>
        </>
      )}
    </FrostedCard>
  );
}
