import { useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import { type InterfaceStats, linuxio } from "@/api";
import DashboardCard, {
  CardHeaderSelect,
  CardStatusDot,
} from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";

import DashboardStatRows from "./DashboardStatRows";
import NetworkGraph from "./NetworkGraph";

const REFETCH_INTERVAL_MS = 1000;

const filterInterfaces = (interfaces: InterfaceStats[]): InterfaceStats[] =>
  interfaces.filter(
    (iface) =>
      !iface.name.startsWith("veth") &&
      !iface.name.startsWith("docker") &&
      !iface.name.startsWith("br") &&
      iface.name !== "lo",
  );

const resolveInterface = (
  interfaces: InterfaceStats[],
  selected: string,
): InterfaceStats | undefined =>
  interfaces.find((iface) => iface.name === selected) ?? interfaces[0];

interface InterfaceSelectionProps {
  selected: string;
}

const NetworkHeader = ({
  onSelect,
  selected,
}: InterfaceSelectionProps & { onSelect: (name: string) => void }) => {
  const selectHeader = useCallback(
    (interfaces: InterfaceStats[]) => {
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
    ...linuxio.network.get_interface_stats,
    refetchInterval: REFETCH_INTERVAL_MS,
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
    (interfaces: InterfaceStats[]) => {
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
    ...linuxio.network.get_interface_stats,
    refetchInterval: REFETCH_INTERVAL_MS,
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
  const selectThroughput = useCallback(
    (interfaces: InterfaceStats[]) => {
      const current = resolveInterface(filterInterfaces(interfaces), selected);

      return current
        ? {
            name: current.name,
            rx: current.rx_speed / 1024,
            tx: current.tx_speed / 1024,
          }
        : null;
    },
    [selected],
  );

  const { data: throughput } = useSuspenseQuery({
    ...linuxio.network.get_interface_stats,
    refetchInterval: REFETCH_INTERVAL_MS,
    select: selectThroughput,
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
