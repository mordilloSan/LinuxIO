import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";

import DashboardStatRows from "./DashboardStatRows";
import NetworkGraph from "./NetworkGraph";

const NetworkInterfacesCard = () => {
  const { data: rawInterfaces } = useSuspenseQuery(
    linuxio.system.get_network_info.queryOptions({
      refetchInterval: 1000,
    }),
  );

  const interfaces = useMemo(
    () =>
      rawInterfaces.map((iface) => ({
        ...iface,
        ipv4: Array.isArray(iface.ipv4) ? iface.ipv4 : [],
        type: iface.name.startsWith("wl")
          ? "wifi"
          : iface.name.startsWith("lo")
            ? "loopback"
            : "ethernet",
      })),
    [rawInterfaces],
  );

  const filteredInterfaces = useMemo(
    () =>
      interfaces.filter(
        (iface) =>
          !iface.name.startsWith("veth") &&
          !iface.name.startsWith("docker") &&
          !iface.name.startsWith("br") &&
          iface.name !== "lo",
      ),
    [interfaces],
  );

  const [selected, setSelected] = useState<string>("");

  const firstName = filteredInterfaces[0]?.name ?? "";
  const selectedExists =
    selected && filteredInterfaces.some((i) => i.name === selected);
  const effectiveSelected = selectedExists ? selected : firstName;

  const selectedInterface = useMemo(
    () => filteredInterfaces.find((i) => i.name === effectiveSelected),
    [filteredInterfaces, effectiveSelected],
  );

  const options = useMemo(
    () =>
      filteredInterfaces.map((iface) => ({
        value: iface.name,
        label: iface.name,
      })),
    [filteredInterfaces],
  );

  const content = selectedInterface ? (
    <DashboardStatRows
      rows={[
        {
          label: "IPv4",
          value: selectedInterface.ipv4?.length
            ? selectedInterface.ipv4.join(", ")
            : "None",
        },
        { label: "MAC", value: selectedInterface.mac },
        { label: "Speed", value: selectedInterface.speed },
      ]}
    />
  ) : (
    <AppTypography variant="body2">No interface selected.</AppTypography>
  );

  const content2 = selectedInterface ? (
    <div style={{ height: "90px", width: "100%", minWidth: 0 }}>
      <NetworkGraph
        interfaceName={effectiveSelected}
        key={effectiveSelected}
        rx={selectedInterface.rx_speed}
        tx={selectedInterface.tx_speed}
      />
    </div>
  ) : (
    <AppTypography variant="body2">No graph data.</AppTypography>
  );

  return (
    <DashboardCard
      avatarIcon="mdi:ethernet"
      connectionStatus={
        selectedInterface?.ipv4 && selectedInterface.ipv4.length > 0
          ? "online"
          : "offline"
      }
      onSelect={(val: string) => {
        setSelected(val);
      }}
      selectedOption={effectiveSelected}
      selectedOptionLabel={effectiveSelected}
      selectOptions={options}
      stats={content}
      stats2={content2}
      title="Network"
    />
  );
};

export default NetworkInterfacesCard;
