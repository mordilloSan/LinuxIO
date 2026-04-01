import React, { useMemo, useState } from "react";

import NetworkGraph from "./NetworkGraph";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

const NetworkInterfacesCard: React.FC = () => {
  const theme = useAppTheme();
  const { data: rawInterfaces = [], isPending: isLoading } =
    linuxio.system.get_network_info.useQuery({
      refetchInterval: 1000,
    });

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
  if (effectiveSelected !== selected) {
    setSelected(effectiveSelected);
  }

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
    isLoading ? (
      <ComponentLoader />
    ) : (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          alignSelf: "flex-start",
          width: "fit-content",
        }}
      >
        {[
          {
            label: "IPv4",
            value: selectedInterface.ipv4?.length
              ? selectedInterface.ipv4.join(", ")
              : "None",
          },
          { label: "MAC", value: selectedInterface.mac },
          { label: "Speed", value: selectedInterface.speed },
        ].map(({ label, value }, index, rows) => (
          <div
            key={label}
            style={{
              display: "flex",
              alignItems: "baseline",
              justifyContent: "flex-start",
              paddingTop: theme.spacing(0.5),
              paddingBottom: theme.spacing(0.5),
              borderBottom:
                index === rows.length - 1
                  ? "none"
                  : "1px solid var(--app-palette-divider)",
              gap: theme.spacing(1),
            }}
          >
            <AppTypography
              variant="caption"
              color="text.secondary"
              style={{
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                fontSize: "0.62rem",
                flexShrink: 0,
              }}
            >
              {label}
            </AppTypography>
            <AppTypography variant="body2" fontWeight={500} noWrap>
              {value}
            </AppTypography>
          </div>
        ))}
      </div>
    )
  ) : (
    <AppTypography variant="body2">No interface selected.</AppTypography>
  );

  const content2 = selectedInterface ? (
    isLoading ? (
      <ComponentLoader />
    ) : (
      <div style={{ height: "90px", width: "100%", minWidth: 0 }}>
        <NetworkGraph
          key={effectiveSelected}
          rx={selectedInterface.rx_speed}
          tx={selectedInterface.tx_speed}
        />
      </div>
    )
  ) : (
    <AppTypography variant="body2">No graph data.</AppTypography>
  );

  return (
    <DashboardCard
      title="Network"
      avatarIcon="mdi:ethernet"
      stats={content}
      stats2={content2}
      selectOptions={options}
      selectedOption={effectiveSelected}
      selectedOptionLabel={effectiveSelected}
      onSelect={(val: string) => {
        setSelected(val);
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
