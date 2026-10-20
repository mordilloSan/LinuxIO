import { useSuspenseQueries } from "@tanstack/react-query";
import { useState, type CSSProperties } from "react";

import {
  CACHE_TTL_MS,
  linuxio,
  type MonitoringLive,
  type NetworkInterface,
  type VMNetwork,
  type VirtualMachine,
} from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppChip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";

import { EntityCard, KeyValues, PillToggle, Throughput } from "./vmCards";
import { normalizeState, stateChipColor } from "./vmShared";

const pageStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-16)",
  minHeight: 0,
};

type View = "host" | "vm";
const VIEWS = [
  { label: "Host", value: "host" },
  { label: "Virtual machines", value: "vm" },
] as const;

type HostRow = {
  iface: string;
  name: string;
  rx?: number;
  speed: string;
  tx?: number;
};
type VMRow = {
  id: string;
  ip: string;
  mac: string;
  model: string;
  rx?: number;
  state: string;
  tx?: number;
  vmName: string;
};

const hostColumns: AppVirtualTableColumnDef<HostRow>[] = [
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => row.original.name,
    meta: { width: "minmax(140px, 1fr)" },
  },
  {
    accessorKey: "iface",
    header: "Network interface",
    cell: ({ row }) => row.original.iface,
    meta: { width: "minmax(140px, 1fr)" },
  },
  {
    accessorKey: "speed",
    header: "Link speed",
    cell: ({ row }) => row.original.speed,
    meta: { width: "140px" },
  },
  {
    id: "rates",
    header: "Received / Sent",
    cell: ({ row }) => <Throughput rx={row.original.rx} tx={row.original.tx} />,
    meta: { width: "220px" },
  },
];

const vmColumns: AppVirtualTableColumnDef<VMRow>[] = [
  {
    accessorKey: "vmName",
    header: "Name",
    cell: ({ row }) => row.original.vmName,
    meta: { width: "minmax(140px, 1fr)" },
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
    meta: { width: "110px" },
  },
  {
    accessorKey: "model",
    header: "Network interface",
    cell: ({ row }) => row.original.model,
    meta: { width: "130px" },
  },
  {
    accessorKey: "mac",
    header: "MAC address",
    cell: ({ row }) => row.original.mac,
    meta: { width: "170px" },
  },
  {
    accessorKey: "ip",
    header: "IP",
    cell: ({ row }) => row.original.ip,
    meta: { width: "minmax(140px, 1fr)" },
  },
  {
    id: "rates",
    header: "Received / Sent",
    cell: ({ row }) => <Throughput rx={row.original.rx} tx={row.original.tx} />,
    meta: { width: "220px" },
  },
];

function networkStatus(network: VMNetwork): {
  text: string;
  tone: "ok" | "warning" | "muted";
} {
  if (!network.active) return { text: "Down", tone: "muted" };
  if (network.type === "bridge" && !network.hasPhysicalUplink)
    return { text: "No physical link", tone: "warning" };
  return { text: "Normal", tone: "ok" };
}

export function NetworkCard({
  hostname,
  interfaces,
  live,
  network,
  vms,
}: {
  hostname: string;
  interfaces: NetworkInterface[];
  live: MonitoringLive;
  network: VMNetwork;
  vms: VirtualMachine[];
}) {
  const [view, setView] = useState<View>("host");
  const isBridge = network.type === "bridge";
  const uplink = network.uplink ?? network.bridge ?? network.name;
  const status = networkStatus(network);
  const vmRows: VMRow[] = vms.flatMap((vm) =>
    (vm.nics ?? [])
      .filter((nic) => nic.network === network.name)
      .map((nic, index) => ({
        id: `${vm.name}-${nic.mac ?? index}`,
        ip: (nic.ipAddresses ?? []).join(", ") || "-",
        mac: nic.mac ?? "-",
        model: nic.model ?? "virtio",
        rx: nic.receiveBytesPerSecond,
        state: vm.state,
        tx: nic.sendBytesPerSecond,
        vmName: vm.name,
      })),
  );
  const hostRow: HostRow = {
    iface: uplink,
    name: hostname,
    rx: live.interfaces?.[uplink]?.rx_bytes_per_sec,
    speed: interfaces.find((iface) => iface.name === uplink)?.speed || "-",
    tx: live.interfaces?.[uplink]?.tx_bytes_per_sec,
  };

  return (
    <EntityCard
      icon="mdi:web"
      name={network.name}
      status={status.text}
      summary={
        <AppTypography fontWeight={600} variant="body2">
          Type: {isBridge ? "Bridge (external)" : "NAT (internal)"}
        </AppTypography>
      }
      tone={status.tone}
    >
      <KeyValues
        items={[
          ["Host", hostname],
          ["Bridge interface", network.bridge ?? network.name],
          ["Uplink", isBridge ? (network.uplink ?? "None") : "Host NAT"],
          ...(network.subnet
            ? ([["Subnet", network.subnet]] as [string, string][])
            : []),
          ["Virtual machines", vmRows.length],
        ]}
      />
      <PillToggle
        ariaLabel={`${network.name} view`}
        onChange={setView}
        options={VIEWS}
        value={view}
      />
      {view === "host" ? (
        <AppVirtualTable
          ariaLabel={`${network.name} host`}
          columns={hostColumns}
          data={[hostRow]}
          enableSorting={false}
          fillAvailable={false}
          getRowId={(row) => row.iface}
          maxHeight={200}
          variant="embedded"
        />
      ) : (
        <AppVirtualTable
          ariaLabel={`${network.name} virtual machines`}
          columns={vmColumns}
          data={vmRows}
          emptyMessage="No virtual machine uses this network."
          enableSorting={false}
          fillAvailable={false}
          getRowId={(row) => row.id}
          maxHeight={300}
          variant="embedded"
        />
      )}
    </EntityCard>
  );
}

const VMNetworksPage = () => {
  // The VM list rides the /vm layout's poll; the live snapshot polls here for
  // the host uplink's throughput.
  const [
    { data: vms },
    { data: networks },
    { data: host },
    { data: interfaces },
    { data: live },
  ] = useSuspenseQueries({
    queries: [
      { ...linuxio.virt.list, refetchOnMount: false },
      linuxio.virt.networks,
      { ...linuxio.system.get_host_info, staleTime: CACHE_TTL_MS.ONE_DAY },
      linuxio.network.get_network_info,
      {
        ...linuxio.monitoring.get_live,
        refetchInterval: DASHBOARD_REFETCH_FAST_MS,
      },
    ],
  });

  if (networks.length === 0) {
    return (
      <AppTypography color="text.secondary" variant="body2">
        No libvirt networks or host bridges.
      </AppTypography>
    );
  }
  return (
    <div style={pageStyle}>
      {networks.map((network) => (
        <NetworkCard
          hostname={host.hostname}
          interfaces={interfaces}
          key={`${network.type}-${network.name}`}
          live={live}
          network={network}
          vms={vms}
        />
      ))}
    </div>
  );
};

export default VMNetworksPage;
