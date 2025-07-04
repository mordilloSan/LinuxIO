import { Button } from "@mui/material";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";
import { toast } from "sonner";

import CreateInterfaceDialog from "./CreateInterfaceDialog";

import axios from "@/utils/axios";

const CreateInterfaceButton = () => {
  const [serverName, setServerName] = useState("wg0");
  const [port, setPort] = useState(51820);
  const [CIDR, setCIDR] = useState("10.10.20.0/24");
  const [peers, setPeers] = useState(1);
  const [nic, setNic] = useState("");
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const [showDialog, setShowDialog] = useState(false);

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

  interface NetworkInterface {
    name: string;
    type: string;
    mac?: string;
  }

  function getPhysicalNICs(data: NetworkInterface[] | undefined): string[] {
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
      .map((nic) => nic.name);
  }
  const queryClient = useQueryClient();
  const handleCreateInterface = async () => {
    setLoading(true);
    setError(null);

    try {
      const body = {
        name: serverName,
        address: [CIDR],
        listen_port: port,
        egress_nic: nic,
        dns: [],
        mtu: 0,
        peers: [],
        num_peers: peers,
      };
      await axios.post("/wireguard/interface", body);

      toast.success(`WireGuard interface '${serverName}' created`);
      setShowDialog(false);
      // This tells React Query to invalidate the query in the dashboard so that it refetchs
      queryClient.invalidateQueries({ queryKey: ["wireguardInterfaces"] });
    } catch (error: any) {
      const msg = error.response?.data?.error || error.message;
      toast.error(`Failed to create interface: ${msg}`);
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  const availableNICs =
    networkLoading || networkError ? [] : getPhysicalNICs(networkData);

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
      />
    </>
  );
};

export default CreateInterfaceButton;
