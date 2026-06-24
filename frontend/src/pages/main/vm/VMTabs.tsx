import { Icon } from "@iconify/react";
import type { CSSProperties } from "react";

import PreflightSummary from "./PreflightSummary";
import {
  DEFAULT_MANAGED_CLOUD_PATH,
  DEFAULT_MANAGED_ISO_PATH,
  IMAGE_PRESETS,
  formatMemory,
  normalizeState,
  stateChipColor,
} from "./vmShared";

import type { VMPreflight, VirtualMachine } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppChip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { type AppTheme, useAppTheme } from "@/theme";

const tabPanelStyle = (theme: AppTheme): CSSProperties => ({
  display: "flex",
  flexDirection: "column",
  gap: theme.spacing(4.5),
  minHeight: 0,
});

const cardGridStyle = (theme: AppTheme): CSSProperties => ({
  display: "grid",
  gap: theme.spacing(3.5),
  gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))",
});

const metricCardStyle = (theme: AppTheme): CSSProperties => ({
  display: "flex",
  flexDirection: "column",
  gap: theme.spacing(2),
  minWidth: 0,
  padding: theme.spacing(3.5),
});

const metricIconStyle = (theme: AppTheme): CSSProperties => ({
  alignItems: "center",
  color: theme.palette.primary.main,
  display: "inline-flex",
});

const secondarySmallTextStyle = (theme: AppTheme): CSSProperties => ({
  color: theme.palette.text.secondary,
  fontSize: "0.75rem",
});

const pathCardStyle = (theme: AppTheme): CSSProperties => ({
  display: "flex",
  flexDirection: "column",
  gap: theme.spacing(2),
  minWidth: 0,
  padding: theme.spacing(3.5),
});

const preflightCardStyle = (theme: AppTheme): CSSProperties => ({
  display: "flex",
  flexDirection: "column",
  gap: theme.spacing(2.5),
  padding: theme.spacing(3),
});

const tableCardStyle: CSSProperties = {
  minWidth: 0,
  overflow: "hidden",
};

const messageListStyle: CSSProperties = {
  margin: 0,
  paddingLeft: 18,
};

const wrappingCodeStyle: CSSProperties = {
  overflowWrap: "anywhere",
};

function StatusMetric({
  detail,
  icon,
  label,
  value,
}: {
  detail?: string;
  icon: string;
  label: string;
  value: string | number;
}) {
  const theme = useAppTheme();

  return (
    <FrostedCard style={metricCardStyle(theme)}>
      <div style={metricIconStyle(theme)}>
        <Icon height={20} icon={icon} width={20} />
      </div>
      <span style={secondarySmallTextStyle(theme)}>{label}</span>
      <strong style={{ fontSize: "1.35rem", lineHeight: 1.2 }}>{value}</strong>
      {detail ? (
        <small style={secondarySmallTextStyle(theme)}>{detail}</small>
      ) : null}
    </FrostedCard>
  );
}

export function VMPreflightCard({ preflight }: { preflight?: VMPreflight }) {
  const theme = useAppTheme();

  return (
    <FrostedCard style={preflightCardStyle(theme)}>
      <PreflightSummary preflight={preflight} />
      {preflight && (preflight.warnings ?? []).length > 0 && (
        <AppAlert severity="warning">
          <AppAlertTitle>Preflight Warnings</AppAlertTitle>
          <ul style={messageListStyle}>
            {(preflight.warnings ?? []).map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </AppAlert>
      )}
      {preflight && (preflight.errors ?? []).length > 0 && (
        <AppAlert severity="error">
          <AppAlertTitle>Preflight Errors</AppAlertTitle>
          <ul style={messageListStyle}>
            {(preflight.errors ?? []).map((error) => (
              <li key={error}>{error}</li>
            ))}
          </ul>
        </AppAlert>
      )}
    </FrostedCard>
  );
}

export function VMDashboardTab({
  preflight,
  vms,
}: {
  preflight?: VMPreflight;
  vms: VirtualMachine[];
}) {
  const running = vms.filter((vm) => vm.state === "running").length;
  const paused = vms.filter((vm) => vm.state === "paused").length;
  const totalMemoryMB = vms.reduce((sum, vm) => sum + vm.memoryMB, 0);
  const totalDiskGB = vms.reduce((sum, vm) => sum + vm.diskGB, 0);
  const theme = useAppTheme();

  return (
    <div style={tabPanelStyle(theme)}>
      <div style={cardGridStyle(theme)}>
        <StatusMetric
          detail={`${running} running, ${paused} paused`}
          icon="mdi:server"
          label="Configured VMs"
          value={vms.length}
        />
        <StatusMetric
          detail={preflight?.defaultNetworkActive ? "default active" : "check"}
          icon="mdi:lan"
          label="Default Network"
          value={preflight?.defaultNetworkExists ? "Ready" : "Missing"}
        />
        <StatusMetric
          detail={preflight?.defaultPoolActive ? "default active" : "check"}
          icon="mdi:database"
          label="Default Pool"
          value={preflight?.defaultPoolExists ? "Ready" : "Missing"}
        />
        <StatusMetric
          detail={`${totalDiskGB} GB provisioned`}
          icon="mdi:memory"
          label="Assigned Memory"
          value={formatMemory(totalMemoryMB)}
        />
      </div>
      <VMPreflightCard preflight={preflight} />
    </div>
  );
}

type VMNetworkRow = {
  id: string;
  ips: string[];
  mac: string;
  model: string;
  network: string;
  state: string;
  vmName: string;
};

export function VMNetworksTab({ vms }: { vms: VirtualMachine[] }) {
  const theme = useAppTheme();
  const rows = vms.flatMap((vm) =>
    (vm.nics ?? []).map((nic, index) => ({
      id: `${vm.name}-${nic.mac || index}`,
      ips: nic.ipAddresses ?? [],
      mac: nic.mac || "-",
      model: nic.model || "virtio",
      network: nic.network || "network",
      state: vm.state,
      vmName: vm.name,
    })),
  );

  const columns: AppDataTableColumnDef<VMNetworkRow>[] = [
    {
      accessorKey: "vmName",
      header: "VM",
      cell: ({ row }) => row.original.vmName,
      meta: { width: "minmax(150px, 1fr)" },
    },
    {
      accessorKey: "state",
      header: "State",
      cell: ({ row }) => (
        <AppChip
          color={stateChipColor(row.original.state)}
          label={normalizeState(row.original.state)}
          size="small"
          variant="soft"
        />
      ),
      meta: { width: "120px" },
    },
    {
      accessorKey: "network",
      header: "Network",
      cell: ({ row }) => row.original.network,
      meta: { width: "minmax(140px, 1fr)" },
    },
    {
      id: "ipAddresses",
      header: "IP",
      cell: ({ row }) => {
        if (row.original.ips.length === 0) {
          return (
            <span style={{ color: theme.palette.text.secondary }}>
              No lease
            </span>
          );
        }
        return row.original.ips.join(", ");
      },
      meta: { width: "minmax(180px, 1fr)" },
    },
    {
      accessorKey: "mac",
      header: "MAC",
      cell: ({ row }) => row.original.mac,
      meta: { width: "minmax(160px, 1fr)" },
    },
  ];

  return (
    <div style={tabPanelStyle(theme)}>
      <FrostedCard style={tableCardStyle}>
        <AppDataTable
          ariaLabel="Virtual machine networks"
          columns={columns}
          data={rows}
          emptyMessage="No virtual network interfaces."
          enableSorting={false}
          getRowId={(row) => row.id}
          variant="embedded"
        />
      </FrostedCard>
    </div>
  );
}

export function VMImagesTab({ preflight }: { preflight?: VMPreflight }) {
  const theme = useAppTheme();
  const isoPath = preflight?.managedPaths?.isos ?? DEFAULT_MANAGED_ISO_PATH;
  const cloudPath =
    preflight?.managedPaths?.cloudImages ?? DEFAULT_MANAGED_CLOUD_PATH;

  return (
    <div style={tabPanelStyle(theme)}>
      <div style={cardGridStyle(theme)}>
        <FrostedCard style={pathCardStyle(theme)}>
          <AppTypography component="div" fontWeight={700} variant="body2">
            ISO folder
          </AppTypography>
          <code style={wrappingCodeStyle}>{isoPath}</code>
        </FrostedCard>
        <FrostedCard style={pathCardStyle(theme)}>
          <AppTypography component="div" fontWeight={700} variant="body2">
            Cloud image folder
          </AppTypography>
          <code style={wrappingCodeStyle}>{cloudPath}</code>
        </FrostedCard>
        <FrostedCard style={pathCardStyle(theme)}>
          <AppTypography component="div" fontWeight={700} variant="body2">
            Custom installers
          </AppTypography>
          <span style={secondarySmallTextStyle(theme)}>ISO installer</span>
        </FrostedCard>
        {IMAGE_PRESETS.map((preset) => (
          <FrostedCard key={preset.id} style={pathCardStyle(theme)}>
            <div>
              <AppTypography component="div" fontWeight={700} variant="body2">
                {preset.label}
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Ready image
              </AppTypography>
            </div>
            <div
              style={{
                ...secondarySmallTextStyle(theme),
                display: "flex",
                flexWrap: "wrap",
                gap: theme.spacing(2),
              }}
            >
              <span>{preset.vcpus} CPU</span>
              <span>{Number.parseInt(preset.memoryMB, 10) / 1024} GB RAM</span>
              <span>{preset.diskGB} GB disk</span>
            </div>
          </FrostedCard>
        ))}
      </div>
    </div>
  );
}
