import { Button } from "@mui/material";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import React, { useEffect, useState, useCallback, useMemo } from "react";
import { toast } from "sonner";

import CreateInterfaceDialog from "./CreateInterfaceDialog";

import axios from "@/utils/axios";

const wireguardToastMeta = {
  meta: { href: "/wireguard", label: "Open WireGuard" },
};

const BASE_CIDR_PREFIX = "10.10."; // Only works for /24
const BASE_CIDR_START = 20;
const BASE_CIDR_SUFFIX = "0/24";

interface NetworkInterface {
  name: string;
  type: string;
  mac?: string;
  ipv4?: string | null;
}

const CreateInterfaceButton = () => {
  const [serverName, setServerName] = useState("");
  const [port, setPort] = useState(0);
  const [CIDR, setCIDR] = useState("");
  const [peers, setPeers] = useState(1);
  const [nic, setNic] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [showDialog, setShowDialog] = useState(false);
  const [dns, setDns] = useState("");

  // Fetch network info
  const {
    data: networkData,
    isLoading: networkLoading,
    error: networkError,
  } = useQuery({
    queryKey: ["networkInfo"],
    queryFn: async () => {
      const res = await axios.get("/network/info");
      return res.data;
    },
  });

  // Fetch existing WireGuard interfaces
  const { data: wgInterfaces } = useQuery({
    queryKey: ["wireguardInterfaces"],
    queryFn: async () => {
      const res = await axios.get("/wireguard/interfaces");
      return res.data;
    },
  });

  // Memoize WireGuard interfaces array
  const wgArray = useMemo(
    () =>
      Array.isArray(wgInterfaces?.interfaces) ? wgInterfaces.interfaces : [],
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

  // Preselect NIC, name, port, and CIDR on dialog open
  useEffect(() => {
    if (showDialog) {
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
    }
  }, [
    showDialog,
    networkData,
    wgArray,
    getPhysicalNICs,
    nextAvailableWgName,
    nextAvailablePort,
    nextAvailableCIDR,
  ]);

  const queryClient = useQueryClient();

  const handleCreateInterface = async () => {
    setLoading(true);
    setError(null);
    try {
      const dnsArray = dns
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);

      const body = {
        name: serverName,
        address: [CIDR],
        listen_port: port,
        egress_nic: nic,
        dns: dnsArray, // send if provided; backend treats [] as "no override"
        mtu: 0,
        peers: [],
        num_peers: peers,
      };
      await axios.post("/wireguard/interface", body);

      toast.success(
        `WireGuard interface '${serverName}' created`,
        wireguardToastMeta,
      );
      setShowDialog(false);
      // optionally reset dns
      setDns("");
      queryClient.invalidateQueries({ queryKey: ["wireguardInterfaces"] });
    } catch (error: any) {
      const msg = error.response?.data?.error || error.message;
      toast.error(`Failed to create interface: ${msg}`, wireguardToastMeta);
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  const availableNICs =
    networkLoading || networkError ? [] : getPhysicalNICs(networkData);

  // Pass down for validation
  const existingNames = wgArray.map((iface: any) => iface.name);
  const existingPorts = wgArray.map((iface: any) => iface.port);
  const existingCIDRs = wgArray.map((iface: any) => iface.address);

  return (
    <>
      <Button
        variant="contained"
        color="primary"
        onClick={() => setShowDialog(true)}
      >
        Create New Interface
      </Button>
      <CreateInterfaceDialog
        open={showDialog}
        onClose={() => setShowDialog(false)}
        onCreate={handleCreateInterface}
        loading={loading}
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
