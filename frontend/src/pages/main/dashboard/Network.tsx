import { Box, Typography } from "@mui/material";
import React, { useMemo, useState, useEffect, useRef } from "react";

import NetworkGraph from "./NetworkGraph";

import linuxio from "@/api/react-query";
import GeneralCard from "@/components/cards/GeneralCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";

const NetworkInterfacesCard: React.FC = () => {
  const { data: rawInterfaces = [], isPending: isLoading } =
    linuxio.system.get_network_info.useQuery({
      refetchInterval: 1000,
    });

  // Transform data to add type field
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

  // Adjust selection during render (no Effect needed)
  const firstName = filteredInterfaces[0]?.name ?? "";
  const selectedExists =
    selected && filteredInterfaces.some((i) => i.name === selected);
  const effectiveSelected = selectedExists ? selected : firstName;
  if (effectiveSelected !== selected) {
    // guarded setState during render is fine; React will immediately re-render
    setSelected(effectiveSelected);
  }

  const selectedInterface = useMemo(
    () => filteredInterfaces.find((i) => i.name === effectiveSelected),
    [filteredInterfaces, effectiveSelected],
  );

  const [history, setHistory] = useState<
    { time: number; rx: number; tx: number }[]
  >([]);
  const lastSampleRef = useRef<number>(0);

  useEffect(() => {
    if (!selectedInterface) return;
    const now = Date.now();

    setHistory((prev) => {
      const last = prev[prev.length - 1];
      const shouldAppend =
        now - lastSampleRef.current > 250 ||
        prev.length === 0 ||
        last?.rx !== selectedInterface.rx_speed ||
        last?.tx !== selectedInterface.tx_speed;

      if (!shouldAppend) return prev;

      lastSampleRef.current = now;
      return [
        ...prev.slice(-29),
        {
          time: now,
          rx: selectedInterface.rx_speed,
          tx: selectedInterface.tx_speed,
        },
      ];
    });
  }, [
    selectedInterface?.rx_speed,
    selectedInterface?.tx_speed,
    selectedInterface,
  ]);

  const options = useMemo(
    () =>
      filteredInterfaces.map((iface) => ({
        value: iface.name,
        label: iface.name,
      })),
    [filteredInterfaces],
  );

  const content = selectedInterface ? (
    isLoading ? (
      <ComponentLoader />
    ) : (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Typography variant="body2">
          <strong>IPv4:</strong>{" "}
          {selectedInterface.ipv4 && selectedInterface.ipv4.length > 0
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
      selectedOption={effectiveSelected}
      selectedOptionLabel={effectiveSelected}
      onSelect={(val: string) => {
        setSelected(val);
        setHistory([]); // reset graph when switching interfaces
      }}
      connectionStatus={
        selectedInterface?.ipv4 && selectedInterface.ipv4.length > 0
          ? "online"
          : "offline"
      }
    />
  );
};

export default NetworkInterfacesCard;
