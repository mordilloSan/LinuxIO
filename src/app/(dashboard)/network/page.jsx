"use client";

import { useQuery } from "@tanstack/react-query";
import React from "react";
import { Grid, Typography } from "@mui/material";
import { formatDataRate } from "@/utils/formatter";
import { useAuthenticatedFetch } from "@/utils/customFetch";
import NetworkInterfaceCard from "@/components/cards/NetworkInterfaceCard";
import LoadingIndicator from "@/components/LoadingIndicator";
import CardWithBorder from "@/components/cards/CardWithBorder";
import dynamic from "next/dynamic";

const NetworkDownloadChart = dynamic(() => import("./NetworkDownloadChart"), {  ssr: false,});

const NetworkUploadChart = dynamic(() => import("./NetworkUploadChart"), {  ssr: false,});

// Utility function to format the IP addresses
const formatIpAddress = (ip4, ip6) => {
  if (Array.isArray(ip4) && ip4.length > 0) {
    return ip4.map((ip) => ip.address).join(", ");
  } else if (Array.isArray(ip6) && ip6.length > 0) {
    return ip6.map((ip) => ip.address).join(", ");
  } else {
    return "N/A";
  }
};

// Utility function to sort interfaces
const sortInterfaces = (interfaces) =>
  interfaces.sort((a, b) => {
    if (a.isPhysical && !b.isPhysical) {
      return -1;
    } else if (!a.isPhysical && b.isPhysical) {
      return 1;
    } else {
      return a.name.localeCompare(b.name);
    }
  });

const NetworkStatsCards = () => {
  const customFetch = useAuthenticatedFetch();
  const { data: networkInfo, isLoading, error } = useQuery({
    queryKey: ["networkInfo"],
    queryFn: () => customFetch(`/api/network/networkinfo`),
    refetchInterval: 1000,
  });

  if (isLoading) {
    return <LoadingIndicator />;
  }

  if (error) {
    return <Typography>Error loading network interfaces.</Typography>;
  }

  const { interfaces = [] } = networkInfo || {};

  // Extract interface data with formatting
  const interfaceData = interfaces.map((details) => {
    const { iface, tx_sec, rx_sec, ip4, ip6, carrierSpeed, vendor, product, description } = details;

    const [formattedTxValue, txUnit] = formatDataRate(tx_sec || 0);
    const [formattedRxValue, rxUnit] = formatDataRate(rx_sec || 0);

    return {
      name: iface || "Unknown Interface",
      ipAddress: formatIpAddress(ip4, ip6),
      tx: formattedTxValue > 0 ? `${formattedTxValue} ${txUnit}` : "N/A",
      rx: formattedRxValue > 0 ? `${formattedRxValue} ${rxUnit}` : "N/A",
      carrierSpeed: carrierSpeed ? `${carrierSpeed}` : "N/A",
      vendor: vendor || "N/A",
      product: product || "N/A",
      description: description || "N/A",
      isPhysical: description && description.toLowerCase().includes("ethernet interface"),
    };
  });

  // Sort interfaces: physical NICs first, then alphabetically by name
  const sortedInterfaces = sortInterfaces(interfaceData);

  if (sortedInterfaces.length === 0) {
    return <Typography>No network interfaces available.</Typography>;
  }

  return (
    <Grid container spacing={2}>
<Grid item xs={12} md={6}>
        <CardWithBorder
          title="Network Activity - Download"
          avatarIcon="ph:network"
          stats={<NetworkDownloadChart />}
        />
      </Grid>
      <Grid item xs={12} md={6}>
        <CardWithBorder
          title="Network Activity - Upload"
          avatarIcon="ph:network"
          stats={<NetworkUploadChart />}
        />
      </Grid>
      {/* A full-width empty Grid item to push the interfaces to the next row */}
      <Grid item xs={12} />
      {sortedInterfaces.map((iface) => (
        <Grid item xs={12} sm={6} md={4} lg={3} key={iface.name}>
          <NetworkInterfaceCard {...iface} />
        </Grid>
      ))}
    </Grid>
  );
};

export default NetworkStatsCards;
