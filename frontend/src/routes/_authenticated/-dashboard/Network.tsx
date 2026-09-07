import { useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import { linuxio, type NetworkInterface } from "@/api";
import DashboardCard, {
  CardHeaderSelect,
  CardStatusDot,
} from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";

import DashboardStatRows from "./DashboardStatRows";
import NetworkGraph from "./NetworkGraph";

const filterInterfaces = (interfaces: NetworkInterface[]): NetworkInterface[] =>
  interfaces.filter(
    (iface) =>
      !iface.name.startsWith("veth") &&
      !iface.name.startsWith("docker") &&
      !iface.name.startsWith("br") &&
      iface.name !== "lo",
  );

const resolveInterface = (
  interfaces: NetworkInterface[],
  selected: string,
): NetworkInterface | undefined =>
  interfaces.find((iface) => iface.name === selected) ?? interfaces[0];

interface InterfaceSelectionProps {
  selected: string;
}

const NetworkHeader = ({
  onSelect,
  selected,
}: InterfaceSelectionProps & { onSelect: (name: string) => void }) => {
  const selectHeader = useCallback(
    (interfaces: NetworkInterface[]) => {
      const filtered = filterInterfaces(interfaces);
      const current = resolveInterface(filtered, selected);

      return {
        names: filtered.map((iface) => iface.name),
        online: Boolean(current?.ipv4?.length),
        selectedName: current?.name ?? "",
      };
    },
    [selected],
  );

  const { data: header } = useSuspenseQuery({
    ...linuxio.network.get_network_info,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: selectHeader,
  });

  return (
    <>
      <CardStatusDot online={header.online} />
      <CardHeaderSelect
        onChange={onSelect}
        options={header.names.map((name) => ({ label: name, value: name }))}
        value={header.selectedName}
      />
    </>
  );
};

const NetworkStats = ({ selected }: InterfaceSelectionProps) => {
  const selectDetails = useCallback(
    (interfaces: NetworkInterface[]) => {
      const current = resolveInterface(filterInterfaces(interfaces), selected);

      return current
        ? {
            ipv4: current.ipv4?.length ? current.ipv4.join(", ") : "None",
            mac: current.mac,
            speed: current.speed,
          }
        : null;
    },
    [selected],
  );

  const { data: details } = useSuspenseQuery({
    ...linuxio.network.get_network_info,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: selectDetails,
  });

  if (!details) {
    return (
      <AppTypography variant="body2">No interface selected.</AppTypography>
    );
  }

  return (
    <DashboardStatRows
      rows={[
        { label: "IPv4", value: details.ipv4 },
        { label: "MAC", value: details.mac },
        { label: "Speed", value: details.speed },
      ]}
    />
  );
};

const NetworkGraphPane = ({ selected }: InterfaceSelectionProps) => {
  const { data: name } = useSuspenseQuery({
    ...linuxio.network.get_network_info,
    select: (interfaces) =>
      resolveInterface(filterInterfaces(interfaces), selected)?.name ?? "",
  });
  const { data: throughput } = useSuspenseQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: (live) => {
      const current = name ? (live.interfaces ?? {})[name] : undefined;
      return current
        ? {
            name,
            rx: current.rx_bytes_per_sec / 1024,
            tx: current.tx_bytes_per_sec / 1024,
          }
        : null;
    },
  });

  if (!throughput) {
    return <AppTypography variant="body2">No graph data.</AppTypography>;
  }

  return (
    <div style={{ height: "90px", width: "100%", minWidth: 0 }}>
      <NetworkGraph
        interfaceName={throughput.name}
        key={throughput.name}
        rx={throughput.rx}
        tx={throughput.tx}
      />
    </div>
  );
};

const NetworkInterfacesCard = () => {
  const [selected, setSelected] = useState("");

  return (
    <DashboardCard
      avatarIcon="mdi:ethernet"
      headerExtras={
        <NetworkHeader onSelect={setSelected} selected={selected} />
      }
      stats={<NetworkStats selected={selected} />}
      stats2={<NetworkGraphPane selected={selected} />}
      title="Network"
    />
  );
};

export default NetworkInterfacesCard;
