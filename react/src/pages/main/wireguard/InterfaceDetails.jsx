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

const InterfaceDetails = ({ params }) => {
  const [qrCode, setQrCode] = useState(null);
  const [openDialog, setOpenDialog] = useState(false);
  const [loadingQr, setLoadingQr] = useState(false);
  const interfaceName = params.id;

  // Fetch peer list for this interface
  const {
    data: peers = [],
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: ["wg-peers", interfaceName],
    queryFn: async () => {
      const res = await axios.get(
        `/wireguard/interface/${interfaceName}/peers`,
      );
      // If the backend returns an object, not an array, fix here!
      return Array.isArray(res.data) ? res.data : res.data.peers || [];
    },
    enabled: !!interfaceName,
  });

  // Optionally, fetch interface info
  // const { data: interfaceData } = useQuery({
  //   queryKey: ["wg-interface", interfaceName],
  //   queryFn: async () => {
  //     const res = await axios.get(`/wireguard/interface/${interfaceName}`);
  //     return res.data;
  //   },
  //   enabled: !!interfaceName,
  // });

  if (isLoading) return <CircularProgress />;
  if (isError)
    return <Typography color="error">Failed to load peer details</Typography>;

  // NOTE: Adjust peer fields as needed to match your backend
  const handleDeletePeer = async (publicKey) => {
    try {
      await axios.delete(
        `/wireguard/interface/${interfaceName}/peer/${publicKey}`,
      );
      refetch();
    } catch (error) {
      console.error("Failed to delete peer:", error);
    }
  };

  const handleDownloadConfig = (publicKey) => {
    // Adjust this route as per your backend
    window.location.href = `/wireguard/interface/${interfaceName}/peer/${publicKey}/config`;
  };

  const handleViewQrCode = async (publicKey) => {
    setLoadingQr(true);
    try {
      // Adjust this route as per your backend
      const res = await axios.get(
        `/wireguard/interface/${interfaceName}/peer/${publicKey}/qrcode`,
      );
      setQrCode(res.data.qrcode); // assuming backend returns { qrcode: "data:image/png;base64,..." }
      setOpenDialog(true);
    } catch (error) {
      console.error("Failed to fetch QR code:", error);
    } finally {
      setLoadingQr(false);
    }
  };

  return (
    <>
      <Grid container spacing={3}>
        {peers.length === 0 ? (
          <Grid item xs={12}>
            <Typography>No peers found for this interface.</Typography>
          </Grid>
        ) : (
          peers.map((peer, idx) => (
            <Grid
              item
              xs={12}
              sm={6}
              md={6}
              lg={4}
              key={peer.public_key || idx}
            >
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
                        sx={{ color: "red" }}
                      >
                        <Delete />
                      </IconButton>
                      <IconButton
                        aria-label="Download Config"
                        onClick={() => handleDownloadConfig(peer.public_key)}
                      >
                        <GetApp />
                      </IconButton>
                      <IconButton
                        aria-label="View QR Code"
                        onClick={() => handleViewQrCode(peer.public_key)}
                      >
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
                    Keep Alive: {peer.persistent_keepalive || "-"}
                  </Typography>
                  {/* Add more fields if needed */}
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
        }}
      >
        <DialogContent>
          {loadingQr ? (
            <Typography>Loading QR code...</Typography>
          ) : qrCode ? (
            // <Image src={qrCode} alt="QR Code" width={300} height={300} style={{ width: "100%", height: "auto" }} />
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
