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
} from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { useState } from "react";
import axios from "@/utils/axios";

type Peer = {
  public_key: string;
  allowed_ips?: string[];
  endpoint?: string;
  preshared_key?: string;
  persistent_keepalive?: number;
};

interface InterfaceDetailsProps {
  params: {
    id: string;
  };
}

const InterfaceDetails: React.FC<InterfaceDetailsProps> = ({ params }) => {
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
        `/wireguard/interface/${interfaceName}/peers`
      );
      return Array.isArray(res.data) ? res.data : res.data.peers || [];
    },
    enabled: !!interfaceName,
  });

  const handleDeletePeer = async (publicKey: string) => {
    try {
      await axios.delete(
        `/wireguard/interface/${interfaceName}/peer/${publicKey}`
      );
      refetch();
    } catch (error) {
      console.error("Failed to delete peer:", error);
    }
  };

  const handleDownloadConfig = (publicKey: string) => {
    window.location.href = `/wireguard/interface/${interfaceName}/peer/${publicKey}/config`;
  };

  const handleViewQrCode = async (publicKey: string) => {
    setLoadingQr(true);
    try {
      const res = await axios.get(
        `/wireguard/interface/${interfaceName}/peer/${publicKey}/qrcode`
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
          peers.map((peer, idx) => (
            <Grid
              size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}
              key={peer.public_key || idx}>
              <Card>
                <CardContent>
                  <Box display="flex" justifyContent="space-between">
                    <Typography variant="h6" sx={{ fontSize: "1.1rem" }}>
                      {peer.public_key?.slice(0, 12) || "Peer"}
                    </Typography>
                    <Box>
                      <IconButton
                        aria-label="Delete"
                        onClick={() => handleDeletePeer(peer.public_key)}
                        sx={{ color: "red" }}>
                        <Delete />
                      </IconButton>
                      <IconButton
                        aria-label="Download Config"
                        onClick={() => handleDownloadConfig(peer.public_key)}>
                        <GetApp />
                      </IconButton>
                      <IconButton
                        aria-label="View QR Code"
                        onClick={() => handleViewQrCode(peer.public_key)}>
                        <QrCode />
                      </IconButton>
                    </Box>
                  </Box>
                  <Typography variant="body2">
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
          ))
        )}
      </Grid>

      <Dialog
        open={openDialog}
        onClose={() => {
          setOpenDialog(false);
          setQrCode(null);
        }}>
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

export default InterfaceDetails;
