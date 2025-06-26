import React, { useState } from "react";
import { Button } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import CreateInterfaceDialog from "./CreateInterfaceDialog";
import axios from "@/utils/axios";

const CreateInterfaceButton = () => {
  const [serverName, setServerName] = useState("wg0");
  const [port, setPort] = useState("51820");
  const [CIDR, setCIDR] = useState("10.10.20.0/24");
  const [peers, setPeers] = useState("1");
  const [nic, setNic] = useState("");
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const [showDialog, setShowDialog] = useState(false);

  // Fetch the WireGuard interfaces
  const { data: WGinterfaces = [], refetch } = useQuery({
    queryKey: ["WGinterfaces"],
    queryFn: async () => {
      const res = await axios.get("/wireguard/interfaces");
      return res.data;
    },
  });

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

  // Function to extract physical NICs
  function getPhysicalNICs(data) {
    if (!Array.isArray(data)) return [];
    return data
      .filter(
        (nic) =>
          nic.type === "ethernet" &&
          nic.name.startsWith("enp") &&
          nic.mac &&
          !nic.name.startsWith("veth") &&
          !nic.name.startsWith("docker") &&
          !nic.name.startsWith("br-")
      )
      .map((nic) => nic.name);
  }

  const handleCreateInterface = async () => {
    setLoading(true);
    setError(null);

    try {
      await axios.post("/wireguard/create", {
        serverName,
        port,
        CIDR,
        peers,
        nic,
      });
      setShowDialog(false);
      refetch();
    } catch (error) {
      setError(error.response?.data?.error || error.message);
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
        onClick={() => setShowDialog(true)}>
        Create New Interface
      </Button>
      <CreateInterfaceDialog
        open={showDialog}
        onClose={() => setShowDialog(false)}
        onCreate={handleCreateInterface}
        loading={loading}
        error={error}
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
