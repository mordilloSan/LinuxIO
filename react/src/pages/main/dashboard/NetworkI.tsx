import { Box, Typography } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { useState, useEffect } from "react";

import NetworkGraph from "./NetworkGraph";

import GeneralCard from "@/components/cards/GeneralCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import axios from "@/utils/axios";

interface InterfaceStats {
  name: string;
  mac: string;
  mtu: number;
  ipv4: string[];
  ipv6: string[];
  rx_speed: number;
  tx_speed: number;
  speed: string;
  state: number;
}

const NetworkInterfacesCard: React.FC = () => {
  const { data: interfaces = [], isLoading } = useQuery<InterfaceStats[]>({
    queryKey: ["networkInterfaces"],
    queryFn: async () => {
      const res = await axios.get("/network/info");
      return res.data.map((iface: any) => ({
        ...iface,
        ipv4: iface.ipv4 ?? [],
        ipv6: iface.ipv6 ?? [],
        type: iface.name.startsWith("wl")
          ? "wifi"
          : iface.name.startsWith("lo")
            ? "loopback"
            : "ethernet",
      }));
    },
    refetchInterval: 1000,
  });

  const [selected, setSelected] = useState("");
  const [history, setHistory] = useState<
    { time: number; rx: number; tx: number }[]
  >([]);

  const filteredInterfaces = interfaces.filter(
    (iface) =>
      !iface.name.startsWith("veth") &&
      !iface.name.startsWith("docker") &&
      iface.name !== "lo",
  );

  useEffect(() => {
    if (filteredInterfaces.length && !selected) {
      setSelected(filteredInterfaces[0].name);
    } else if (
      selected &&
      !filteredInterfaces.some((iface) => iface.name === selected)
    ) {
      setSelected(filteredInterfaces[0]?.name ?? "");
    }
  }, [filteredInterfaces, selected]);

  const selectedInterface = filteredInterfaces.find(
    (iface) => iface.name === selected,
  );

  useEffect(() => {
    if (selectedInterface) {
      setHistory((prev) => [
        ...prev.slice(-29),
        {
          time: Date.now(),
          rx: selectedInterface.rx_speed / 1024,
          tx: selectedInterface.tx_speed / 1024,
        },
      ]);
    }
  }, [selectedInterface]);

  const options = filteredInterfaces.map((iface) => ({
    value: iface.name,
    label: iface.name,
  }));

  const content = selectedInterface ? (
    isLoading ? (
      <ComponentLoader />
    ) : (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Typography variant="body2">
          <strong>IPv4:</strong>{" "}
          {selectedInterface.ipv4.length > 0
            ? selectedInterface.ipv4.join(", ")
            : "None"}
        </Typography>
        <Typography variant="body2">
          <strong>MAC:</strong> {selectedInterface.mac}
        </Typography>
        <Typography variant="body2">
          <strong>Speed:</strong> {selectedInterface.speed}
        </Typography>
      </Box>
    )
  ) : (
    <Typography variant="body2">No interface selected.</Typography>
  );

  const content2 = selectedInterface ? (
    isLoading ? (
      <ComponentLoader />
    ) : (
      <Box sx={{ height: "120px", width: "100%" }}>
        <NetworkGraph data={history} />
      </Box>
    )
  ) : (
    <Typography variant="body2">No graph data.</Typography>
  );

  return (
    <GeneralCard
      title="Network"
      avatarIcon="mdi:ethernet"
      stats={content}
      stats2={content2}
      selectOptions={options}
      selectedOption={selected}
      selectedOptionLabel={selected}
      onSelect={(val: string) => {
        setSelected(val);
        setHistory([]);
      }}
      connectionStatus={
        selectedInterface && selectedInterface.state === 100
          ? "online"
          : "offline"
      }
    />
  );
};

export default NetworkInterfacesCard;
