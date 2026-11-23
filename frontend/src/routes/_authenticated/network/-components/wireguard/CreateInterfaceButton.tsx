import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useState } from "react";

import {
  getMutationErrorMessage,
  linuxio,
  type WireGuardInterface,
} from "@/api";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import { useScopedToast } from "@/hooks/useScopedToast";

import CreateInterfaceDialog, { dnsServers } from "./CreateInterfaceDialog";
import { getEgressNICOptions } from "./wireguardEgressNICs";

const BASE_CIDR_PREFIX = "10.10."; // Only works for /24
const BASE_CIDR_START = 20;
const BASE_CIDR_SUFFIX = "0/24";

const wireguardRouteApi = getRouteApi("/_authenticated/network/wireguard");

const nextAvailableWgName = (existing: string[]): string => {
  let n = 0;
  let candidate = `wg${n}`;
  while (existing.includes(candidate)) {
    n += 1;
    candidate = `wg${n}`;
  }
  return candidate;
};

const nextAvailablePort = (existingPorts: number[], base = 51820): number => {
  let port = base;
  while (existingPorts.includes(port)) {
    port += 1;
  }
  return port;
};

// Parses 10.10.X.0/24 and returns X
const parseCidrThirdOctet = (cidr: string): number | null => {
  const match = cidr.match(/^10\.10\.(\d+)\.0\/24$/);
  return match ? parseInt(match[1], 10) : null;
};

const nextAvailableCIDR = (existingCIDRs: string[]): string => {
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
};

interface CreateInterfaceButtonProps {
  interfaces: WireGuardInterface[];
}

const CreateInterfaceButton = ({ interfaces }: CreateInterfaceButtonProps) => {
  const toast = useScopedToast({
    label: "Open WireGuard",
    to: "/network/wireguard",
  });
  const [serverName, setServerName] = useState("");
  const [port, setPort] = useState(0);
  const [CIDR, setCIDR] = useState("");
  const [peers, setPeers] = useState(1);
  const [nic, setNic] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [showDialog, setShowDialog] = useState(false);
  const [dns, setDns] = useState("");
  const [host, setHost] = useState("");
  const [mtu, setMtu] = useState("");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const queryClient = useQueryClient();
  const navigate = wireguardRouteApi.useNavigate();

  // Network data is only needed by the create workflow. Keeping this observer
  // disabled until the dialog opens avoids a page-level speculative request.
  const {
    data: networkData,
    isPending: networkPending,
    error: networkError,
  } = useQuery({
    ...linuxio.network.get_network_info,
    enabled: showDialog,
  });

  // Task action for adding an interface; invalidation comes from the
  // OPERATION_QUERY_INVALIDATIONS manifest.
  const { mutate: addInterface, isPending: isAddingInterface } =
    linuxio.wireguard.add_interface.useMutation({
      error: (error) => {
        setError(
          getMutationErrorMessage(
            error,
            "Failed to create WireGuard interface",
          ),
        );
      },
    });

  // Preselect values derived from the already observed route-level interface
  // list. The NIC falls back to the first suitable result from the
  // dialog-owned network query without synchronizing derived state.
  const handleOpenDialog = () => {
    const names = interfaces.map((iface) => iface.name);
    const ports = interfaces.map((iface) => iface.port);
    const cidrs = interfaces.map((iface) => iface.address);

    setServerName(nextAvailableWgName(names));
    setPort(nextAvailablePort(ports));
    setCIDR(nextAvailableCIDR(cidrs));
    setNic("");
    setAdvancedOpen(false);
    setError(null);
    setShowDialog(true);
  };

  // The new interface is selected so its devices are listed, and the first
  // device's QR code opens (?qr=) so a phone can scan it at once. The peer id
  // comes from the interface's own peer list; when that read fails the
  // interface is still selected and the code is one click away.
  const showCreatedInterface = async (name: string, devices: number) => {
    let qr: string | undefined;
    if (devices > 0) {
      try {
        const createdPeers = await queryClient.query({
          ...linuxio.wireguard.list_peers({ interfaceName: name }),
          staleTime: 0,
        });
        qr = createdPeers[0]?.id;
      } catch {
        qr = undefined;
      }
    }
    await navigate({
      to: ".",
      search: (previous) => ({ ...previous, iface: name, qr }),
    });
  };

  const availableNICs = getEgressNICOptions(networkData);
  const firstOnlineNIC = availableNICs.find(
    (candidate) => !candidate.label.includes("disconnected"),
  );
  const selectedNIC =
    nic || firstOnlineNIC?.name || availableNICs[0]?.name || "";

  const handleCreateInterface = () => {
    setError(null);

    const dnsStr = dnsServers(dns).join(",");

    addInterface(
      {
        name: serverName,
        addresses: CIDR,
        listenPort: String(port),
        egressNic: selectedNIC,
        host: host.trim() || undefined,
        dns: dnsStr,
        mtu: mtu.trim() || "0",
        numPeers: String(peers),
      },
      {
        onSuccess: (_result, variables) => {
          toast.success(`WireGuard interface '${variables.name}' created`);
          setShowDialog(false);
          setDns("");
          setHost("");
          setMtu("");
          void showCreatedInterface(
            variables.name,
            Number(variables.numPeers),
          ).catch(() => undefined);
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
      <AppActionIconButton
        ariaLabel="Create New Interface"
        icon="mdi:plus"
        iconSize={20}
        label="Create New Interface"
        onClick={handleOpenDialog}
      />
      <CreateInterfaceDialog
        advancedOpen={advancedOpen}
        availableNICs={availableNICs}
        CIDR={CIDR}
        dns={dns}
        error={error ?? networkError?.message}
        existingCIDRs={existingCIDRs}
        existingNames={existingNames}
        existingPorts={existingPorts}
        host={host}
        loading={isAddingInterface}
        mtu={mtu}
        nic={selectedNIC}
        onClose={() => setShowDialog(false)}
        onCreate={handleCreateInterface}
        open={showDialog}
        optionsLoading={networkPending}
        peers={peers}
        port={port}
        serverName={serverName}
        setAdvancedOpen={setAdvancedOpen}
        setCIDR={setCIDR}
        setDns={setDns}
        setHost={setHost}
        setMtu={setMtu}
        setNic={setNic}
        setPeers={setPeers}
        setPort={setPort}
        setServerName={setServerName}
      />
    </>
  );
};

export default CreateInterfaceButton;
