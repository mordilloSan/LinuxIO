import { Button } from "@mui/material";
import React, { useState, useCallback, useMemo } from "react";
import { toast } from "sonner";

import CreateInterfaceDialog from "./CreateInterfaceDialog";

import type { NetworkInterface } from "@/api/linuxio-types";
import linuxio from "@/api/react-query";

const wireguardToastMeta = {
  meta: { href: "/wireguard", label: "Open WireGuard" },
};

const BASE_CIDR_PREFIX = "10.10."; // Only works for /24
const BASE_CIDR_START = 20;
const BASE_CIDR_SUFFIX = "0/24";

const CreateInterfaceButton = () => {
  const [serverName, setServerName] = useState("");
  const [port, setPort] = useState(0);
  const [CIDR, setCIDR] = useState("");
  const [peers, setPeers] = useState(1);
  const [nic, setNic] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [showDialog, setShowDialog] = useState(false);
  const [dns, setDns] = useState("");

  // Fetch network info via stream API
  const {
    data: networkData,
    isPending: networkLoading,
    error: networkError,
  } = linuxio.dbus.GetNetworkInfo.useQuery();

  // Fetch existing WireGuard interfaces via stream API
  const { data: wgInterfaces, refetch: refetchInterfaces } =
    linuxio.wireguard.list_interfaces.useQuery();

  // Mutation for adding interface
  const addInterfaceMutation = linuxio.wireguard.add_interface.useMutation();

  // Memoize WireGuard interfaces array
  const wgArray = useMemo(
    () => (Array.isArray(wgInterfaces) ? wgInterfaces : []),
    [wgInterfaces],
  );

  // Memoize helper to get physical NICs
  const getPhysicalNICs = useCallback(
    (
      data: NetworkInterface[] | undefined,
    ): { name: string; label: string }[] => {
      if (!Array.isArray(data)) return [];
      return data
        .filter(
          (nic) =>
            nic.type === "ethernet" &&
            nic.name.startsWith("enp") &&
            nic.mac &&
            !nic.name.startsWith("veth") &&
            !nic.name.startsWith("docker") &&
            !nic.name.startsWith("br-"),
        )
        .map((nic) => {
          const ip =
            Array.isArray(nic.ipv4) && nic.ipv4.length > 0
              ? nic.ipv4[0]
              : "disconnected";
          return {
            name: nic.name,
            label: `${nic.name} (${ip})`,
          };
        });
    },
    [],
  );

  const nextAvailableWgName = useCallback((existing: string[]): string => {
    let n = 0;
    let candidate = `wg${n}`;
    while (existing.includes(candidate)) {
      n += 1;
      candidate = `wg${n}`;
    }
    return candidate;
  }, []);

  const nextAvailablePort = useCallback(
    (existingPorts: number[], base = 51820): number => {
      let port = base;
      while (existingPorts.includes(port)) {
        port += 1;
      }
      return port;
    },
    [],
  );

  const parseCidrThirdOctet = useCallback((cidr: string): number | null => {
    // Parses 10.10.X.0/24 and returns X
    const match = cidr.match(/^10\.10\.(\d+)\.0\/24$/);
    return match ? parseInt(match[1], 10) : null;
  }, []);

  const nextAvailableCIDR = useCallback(
    (existingCIDRs: string[]): string => {
      let octet = BASE_CIDR_START;
      let candidate = `${BASE_CIDR_PREFIX}${octet}.${BASE_CIDR_SUFFIX}`;
      const usedOctets = existingCIDRs
        .map(parseCidrThirdOctet)
        .filter((n): n is number => typeof n === "number");

      while (usedOctets.includes(octet)) {
        octet += 10; // Step by 10 (for 20, 30, 40, ...)
        candidate = `${BASE_CIDR_PREFIX}${octet}.${BASE_CIDR_SUFFIX}`;
      }
      return candidate;
    },
    [parseCidrThirdOctet],
  );

  // Preselect NIC, name, port, and CIDR when opening dialog
  const handleOpenDialog = useCallback(() => {
    // Set NIC
    const availableNICs = getPhysicalNICs(networkData);
    if (availableNICs.length > 0) {
      const firstOnline = availableNICs.find(
        (nic) => !nic.label.includes("disconnected"),
      );
      setNic(firstOnline ? firstOnline.name : availableNICs[0].name);
    }
    // Set name/port/CIDR from WireGuard interfaces
    const names = wgArray.map((iface: any) => iface.name);
    const ports = wgArray.map((iface: any) => iface.port);
    const cidrs = wgArray.map((iface: any) => iface.address);

    setServerName(nextAvailableWgName(names));
    setPort(nextAvailablePort(ports));
    setCIDR(nextAvailableCIDR(cidrs));

    setShowDialog(true);
  }, [
    networkData,
    wgArray,
    getPhysicalNICs,
    nextAvailableWgName,
    nextAvailablePort,
    nextAvailableCIDR,
  ]);

  const handleCreateInterface = () => {
    setError(null);

    const dnsStr = dns
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean)
      .join(",");

    // AddInterface expects: [name, addresses, listenPort, egressNic, dns, mtu, peers_json, numPeers]
    const args = [
      serverName,
      CIDR, // addresses as comma-separated string
      String(port),
      nic,
      dnsStr, // dns as comma-separated string
      "0", // mtu
      "[]", // peers_json (empty array)
      String(peers), // numPeers
    ];

    addInterfaceMutation.mutate(args, {
      onSuccess: () => {
        toast.success(
          `WireGuard interface '${serverName}' created`,
          wireguardToastMeta,
        );
        setShowDialog(false);
        setDns("");
        refetchInterfaces();
      },
      onError: (error: any) => {
        const msg = error.message || "Unknown error";
        setError(msg);
      },
    });
  };

  const availableNICs =
    networkLoading || networkError ? [] : getPhysicalNICs(networkData);

  // Pass down for validation
  const existingNames = wgArray.map((iface: any) => iface.name);
  const existingPorts = wgArray.map((iface: any) => iface.port);
  const existingCIDRs = wgArray.map((iface: any) => iface.address);

  return (
    <>
      <Button variant="contained" color="primary" onClick={handleOpenDialog}>
        Create New Interface
      </Button>
      <CreateInterfaceDialog
        open={showDialog}
        onClose={() => setShowDialog(false)}
        onCreate={handleCreateInterface}
        loading={addInterfaceMutation.isPending}
        error={error || undefined}
        serverName={serverName}
        setServerName={setServerName}
        port={port}
        setPort={setPort}
        CIDR={CIDR}
        setCIDR={setCIDR}
        peers={peers}
        setPeers={setPeers}
        nic={nic}
        setNic={setNic}
        availableNICs={availableNICs}
        existingNames={existingNames}
        existingPorts={existingPorts}
        existingCIDRs={existingCIDRs}
        dns={dns}
        setDns={setDns}
      />
    </>
  );
};

export default CreateInterfaceButton;
