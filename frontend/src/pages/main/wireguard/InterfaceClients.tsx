import { Delete, GetApp, QrCode } from "@mui/icons-material";
import {
  Grid,
  Card,
  CardContent,
  Typography,
  Box,
  IconButton,
  CircularProgress,
  Dialog,
  DialogContent,
  Chip,
  Tooltip,
} from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { useState } from "react";
import { toast } from "sonner";

import axios from "@/utils/axios";

type Peer = {
  name: string;
  public_key: string;
  allowed_ips?: string[];
  endpoint?: string;
  preshared_key?: string;
  persistent_keepalive?: number;

  // New fields from server:
  last_handshake?: string; // RFC3339 or "never"
  last_handshake_unix?: number; // 0 if never
  rx_bytes?: number;
  tx_bytes?: number;
  rx_bps?: number; // bytes/sec
  tx_bps?: number; // bytes/sec
};

interface InterfaceDetailsProps {
  params: {
    id: string;
  };
}

// --- small format helpers ---
const formatBytes = (n?: number) => {
  if (n == null) return "-";
  const abs = Math.abs(n);
  if (abs < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let i = -1;
  let val = n;
  do {
    val /= 1024;
    i++;
  } while (Math.abs(val) >= 1024 && i < units.length - 1);
  return `${val.toFixed(val >= 100 ? 0 : val >= 10 ? 1 : 2)} ${units[i]}`;
};

const formatBps = (n?: number) => {
  if (n == null) return "-";
  // bytes/sec formatting
  const abs = Math.abs(n);
  if (abs < 1024) return `${n.toFixed(0)} B/s`;
  const units = ["KiB/s", "MiB/s", "GiB/s", "TiB/s"];
  let i = -1;
  let val = n;
  do {
    val /= 1024;
    i++;
  } while (Math.abs(val) >= 1024 && i < units.length - 1);
  return `${val.toFixed(val >= 100 ? 0 : val >= 10 ? 1 : 2)} ${units[i]}`;
};

const formatAgo = (unix?: number) => {
  if (!unix) return "never";
  const diff = Math.max(0, Math.floor(Date.now() / 1000 - unix));
  if (diff < 60) return `${diff}s ago`;
  const m = Math.floor(diff / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  return `${d}d ago`;
};

const InterfaceClients: React.FC<InterfaceDetailsProps> = ({ params }) => {
  const [qrCode, setQrCode] = useState<string | null>(null);
  const [openDialog, setOpenDialog] = useState(false);
  const [loadingQr, setLoadingQr] = useState(false);

  const interfaceName = params.id;

  const {
    data: peers = [],
    isLoading,
    isError,
    refetch,
  } = useQuery<Peer[]>({
    queryKey: ["wg-peers", interfaceName],
    queryFn: async () => {
      const res = await axios.get(
        `/wireguard/interface/${interfaceName}/peers`,
      );
      return Array.isArray(res.data) ? res.data : res.data.peers || [];
    },
    enabled: !!interfaceName,
    // poll so bps updates
    refetchInterval: 3000,
    refetchOnWindowFocus: false,
  });

  const handleDeletePeer = async (peerName: string) => {
    try {
      await axios.delete(
        `/wireguard/interface/${interfaceName}/peer/${peerName}`,
      );
      toast.success(`WireGuard Peer '${peerName}' deleted`);
      refetch();
    } catch {
      toast.error(`Failed to delete interface: ${peerName}`);
    }
  };

  const handleDownloadConfig = async (peername: string) => {
    try {
      const res = await axios.get(
        `/wireguard/interface/${interfaceName}/peer/${peername}/config`,
        { responseType: "blob" },
      );
      const url = window.URL.createObjectURL(new Blob([res.data]));
      const link = document.createElement("a");
      link.href = url;
      link.setAttribute("download", `${peername}.conf`);
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.URL.revokeObjectURL(url);
    } catch (err) {
      console.error("Download failed", err);
    }
  };

  const handleViewQrCode = async (peername: string) => {
    setLoadingQr(true);
    try {
      const res = await axios.get(
        `/wireguard/interface/${interfaceName}/peer/${peername}/qrcode`,
      );
      setQrCode(res.data.qrcode);
      setOpenDialog(true);
    } catch (error) {
      console.error("Failed to fetch QR code:", error);
    } finally {
      setLoadingQr(false);
    }
  };

  if (isLoading) return <CircularProgress />;
  if (isError)
    return <Typography color="error">Failed to load peer details</Typography>;

  return (
    <>
      <Grid container spacing={3}>
        {peers.length === 0 ? (
          <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3, xl: 2 }}>
            <Typography>No peers found for this interface.</Typography>
          </Grid>
        ) : (
          peers.map((peer, idx) => {
            const lastUnix = peer.last_handshake_unix ?? 0;
            const isOnline = lastUnix > 0 && Date.now() / 1000 - lastUnix < 180; // 3 min window

            return (
              <Grid
                size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}
                key={peer.name || idx}
              >
                <Card>
                  <CardContent>
                    <Box
                      display="flex"
                      justifyContent="space-between"
                      alignItems="center"
                    >
                      <Box display="flex" alignItems="center" gap={1}>
                        <Typography variant="h6" sx={{ fontSize: "1.1rem" }}>
                          {peer.name || "Peer"}
                        </Typography>
                        <Tooltip
                          title={
                            isOnline
                              ? "Handshake < 3 minutes"
                              : "No recent handshake"
                          }
                        >
                          <Chip
                            size="small"
                            label={isOnline ? "Online" : "Offline"}
                            color={isOnline ? "success" : "default"}
                            variant={isOnline ? "filled" : "outlined"}
                          />
                        </Tooltip>
                      </Box>
                      <Box>
                        <IconButton
                          aria-label="Delete"
                          onClick={() => handleDeletePeer(peer.name)}
                          sx={{ color: "red" }}
                        >
                          <Delete />
                        </IconButton>
                        <IconButton
                          aria-label="Download Config"
                          onClick={() => handleDownloadConfig(peer.name)}
                        >
                          <GetApp />
                        </IconButton>
                        <IconButton
                          aria-label="View QR Code"
                          onClick={() => handleViewQrCode(peer.name)}
                        >
                          <QrCode />
                        </IconButton>
                      </Box>
                    </Box>

                    <Typography variant="body2" sx={{ mt: 0.5 }}>
                      Handshake: {formatAgo(peer.last_handshake_unix)}
                    </Typography>

                    <Typography variant="body2">
                      Rx: {formatBytes(peer.rx_bytes)}{" "}
                      <span style={{ opacity: 0.7 }}>
                        ({formatBps(peer.rx_bps)})
                      </span>
                    </Typography>

                    <Typography variant="body2">
                      Tx: {formatBytes(peer.tx_bytes)}{" "}
                      <span style={{ opacity: 0.7 }}>
                        ({formatBps(peer.tx_bps)})
                      </span>
                    </Typography>

                    <Typography variant="body2" sx={{ mt: 1 }}>
                      Allowed IPs:{" "}
                      {(peer.allowed_ips && peer.allowed_ips.join(", ")) || "-"}
                    </Typography>
                    <Typography variant="body2" sx={{ wordBreak: "break-all" }}>
                      Endpoint: {peer.endpoint || "-"}
                    </Typography>
                    <Typography variant="body2" sx={{ wordBreak: "break-all" }}>
                      Preshared Key: {peer.preshared_key || "-"}
                    </Typography>
                    <Typography variant="body2">
                      Keep Alive: {peer.persistent_keepalive ?? "-"}
                    </Typography>
                  </CardContent>
                </Card>
              </Grid>
            );
          })
        )}
      </Grid>

      <Dialog
        open={openDialog}
        onClose={() => {
          setOpenDialog(false);
          setQrCode(null);
        }}
      >
        <DialogContent>
          {loadingQr ? (
            <Typography>Loading QR code...</Typography>
          ) : qrCode ? (
            <img
              src={qrCode}
              alt="QR Code"
              style={{
                width: 300,
                height: 300,
                maxWidth: "100%",
                display: "block",
              }}
            />
          ) : (
            <Typography>Failed to load QR code</Typography>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
};

export default InterfaceClients;
