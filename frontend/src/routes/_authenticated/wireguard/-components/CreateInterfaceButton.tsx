import { useQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import { linuxio, type NetworkInterface, type WireGuardInterface } from "@/api";
import AppButton from "@/components/ui/AppButton";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

import CreateInterfaceDialog from "./CreateInterfaceDialog";

const BASE_CIDR_PREFIX = "10.10."; // Only works for /24
const BASE_CIDR_START = 20;
const BASE_CIDR_SUFFIX = "0/24";

interface CreateInterfaceButtonProps {
  interfaces: WireGuardInterface[];
}

const CreateInterfaceButton = ({ interfaces }: CreateInterfaceButtonProps) => {
  const toast = useScopedToast({
    label: "Open WireGuard",
    to: "/wireguard",
  });
  const [serverName, setServerName] = useState("");
  const [port, setPort] = useState(0);
  const [CIDR, setCIDR] = useState("");
  const [peers, setPeers] = useState(1);
  const [nic, setNic] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [showDialog, setShowDialog] = useState(false);
  const [dns, setDns] = useState("");

  // Network data is only needed by the create workflow. Keeping this observer
  // disabled until the dialog opens avoids a page-level speculative request.
  const {
    data: networkData,
    isPending: networkPending,
    error: networkError,
  } = useQuery(
    linuxio.network.get_network_info.queryOptions({
      enabled: showDialog,
    }),
  );

  // Job action for adding an interface; invalidation comes from the
  // JOB_QUERY_INVALIDATIONS manifest.
  const { mutate: addInterface, isPending: isAddingInterface } =
    linuxio.wireguard.add_interface.useJobAction({
      error: (error) => {
        setError(
          getMutationErrorMessage(
            error,
            "Failed to create WireGuard interface",
          ),
        );
      },
    });

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

  // Preselect values derived from the already observed route-level interface
  // list. The NIC falls back to the first suitable result from the
  // dialog-owned network query without synchronizing derived state.
  const handleOpenDialog = useCallback(() => {
    const names = interfaces.map((iface) => iface.name);
    const ports = interfaces.map((iface) => iface.port);
    const cidrs = interfaces.map((iface) => iface.address);

    setServerName(nextAvailableWgName(names));
    setPort(nextAvailablePort(ports));
    setCIDR(nextAvailableCIDR(cidrs));
    setNic("");
    setError(null);
    setShowDialog(true);
  }, [interfaces, nextAvailableWgName, nextAvailablePort, nextAvailableCIDR]);

  const availableNICs = networkData ? getPhysicalNICs(networkData) : [];
  const firstOnlineNIC = availableNICs.find(
    (candidate) => !candidate.label.includes("disconnected"),
  );
  const selectedNIC =
    nic || firstOnlineNIC?.name || availableNICs[0]?.name || "";

  const handleCreateInterface = () => {
    setError(null);

    const dnsStr = dns
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean)
      .join(",");

    addInterface(
      {
        name: serverName,
        addresses: CIDR,
        listenPort: String(port),
        egressNic: selectedNIC,
        dns: dnsStr,
        mtu: "0",
        peersJson: "[]",
        numPeers: String(peers),
      },
      {
        onSuccess: () => {
          toast.success(`WireGuard interface '${serverName}' created`);
          setShowDialog(false);
          setDns("");
        },
      },
    );
  };

  // Pass down for validation
  const existingNames = interfaces.map((iface) => iface.name);
  const existingPorts = interfaces.map((iface) => iface.port);
  const existingCIDRs = interfaces.map((iface) => iface.address);

  return (
    <>
      <AppButton color="primary" onClick={handleOpenDialog} variant="contained">
        Create New Interface
      </AppButton>
      <CreateInterfaceDialog
        availableNICs={availableNICs}
        CIDR={CIDR}
        dns={dns}
        error={error ?? networkError?.message}
        existingCIDRs={existingCIDRs}
        existingNames={existingNames}
        existingPorts={existingPorts}
        loading={isAddingInterface}
        nic={selectedNIC}
        onClose={() => setShowDialog(false)}
        onCreate={handleCreateInterface}
        open={showDialog}
        optionsLoading={networkPending}
        peers={peers}
        port={port}
        serverName={serverName}
        setCIDR={setCIDR}
        setDns={setDns}
        setNic={setNic}
        setPeers={setPeers}
        setPort={setPort}
        setServerName={setServerName}
      />
    </>
  );
};

export default CreateInterfaceButton;
