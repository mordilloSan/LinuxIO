"use client";

import { useQuery } from "@tanstack/react-query";
import React, { useState } from "react";
import {
  Box,
  Card,
  CardContent,
  Typography,
  Grid,
  Divider,
  Switch,
  IconButton,
} from "@mui/material";
import LoadingIndicator from "@/components/LoadingIndicator";
import { useAuthenticatedFetch } from "@/utils/customFetch";
import EditIcon from "@mui/icons-material/Edit";
import IPv4SettingsDialog from "./IPv4SettingsDialog";

const NetworkDetails = ({ params }) => {
  const [dialogOpen, setDialogOpen] = useState(false);
  const [nicEnabled, setNicEnabled] = useState(true);
  const name = params.Id;

  const customFetch = useAuthenticatedFetch();
  const { data: networkInfo, isLoading, error } = useQuery({
    queryKey: ["networkInfo"],
    queryFn: () => customFetch(`/api/network/networkinfo`),
    refetchInterval: 6000,
  });

  const handleOpenDialog = () => setDialogOpen(true);
  const handleCloseDialog = () => setDialogOpen(false);

  const handleSaveSettings = (settings) => {
    console.log("Saved settings:", settings);
  };

  const handleToggle = async () => {
    try {
      await fetch(`/api/network/${name}/toggle`, {
        method: "POST",
        body: JSON.stringify({ enabled: !nicEnabled }),
      });
      setNicEnabled(!nicEnabled);
    } catch (error) {
      console.error("Failed to toggle NIC status", error);
    }
  };

  if (isLoading) return <LoadingIndicator />;

  if (error) return <Typography>Error loading network info</Typography>;

  // Find the specific network interface by its 'iface' property
  const nicDetails = networkInfo?.interfaces?.find((iface) => iface.iface === name);

  if (!nicDetails) {
    return (
      <Box
        mb={1.5}
        display="flex"
        justifyContent="center"
        alignItems="center"
        height="100%"
      >
        <Typography variant="h6">
          Network interface not found - {name}
        </Typography>
      </Box>
    );
  }

  const isConnected = nicDetails.ip4.length > 0 || nicDetails.ip6.length > 0;

  return (
    <Box padding={2}>
      <Card>
        <CardContent>
          <Grid container spacing={2} alignItems="center">
            <Grid item xs={12} md={6}>
              <Typography variant="h5" gutterBottom>
                {name}
                <Switch
                  checked={nicEnabled}
                  onChange={handleToggle}
                  color="primary"
                  inputProps={{
                    "aria-label": "Enable/Disable Network Interface",
                  }}
                />
                <Typography variant="subtitle1" component="span">
                  {nicEnabled ? "Enabled" : "Disabled"}
                </Typography>
              </Typography>
            </Grid>
            <Grid item xs={12} md={6} style={{ textAlign: "right" }}>
              <Typography variant="body1" color="textSecondary">
                {`${nicDetails.vendor || "Unknown Vendor"} ${nicDetails.product || ""}`}
              </Typography>
            </Grid>
            <Grid item xs={12}>
              <Divider />
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="subtitle2" gutterBottom>
                Status:
              </Typography>
              <Typography variant="body1" color="textSecondary">
                {isConnected ? "Connected" : "Not Connected"}
              </Typography>
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="subtitle2" gutterBottom>
                Carrier:
              </Typography>
              <Typography variant="body1" color="textSecondary">
                {nicDetails.carrierSpeed || "N/A"} Mbps
              </Typography>
            </Grid>
            <Grid item xs={12}>
              <Divider />
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="subtitle2" gutterBottom>
                IPv4:
              </Typography>
              <Typography variant="body1" color="textSecondary">
                Address {nicDetails.ip4[0]?.address || "N/A"}/
                {nicDetails.ip4[0]?.prefixLength || "N/A"}{" "}
                <IconButton size="small" onClick={handleOpenDialog}>
                  <EditIcon fontSize="small" />
                </IconButton>
              </Typography>
              <IPv4SettingsDialog
                open={dialogOpen}
                handleClose={handleCloseDialog}
                handleSave={handleSaveSettings}
              />
            </Grid>

            <Grid item xs={12} md={6}>
              <Typography variant="subtitle2" gutterBottom>
                IPv6:
              </Typography>
              <Typography variant="body1" color="textSecondary">
                Address {nicDetails.ip6[0]?.address || "Ignore"}{" "}
                <IconButton size="small">
                  <EditIcon fontSize="small" />
                </IconButton>
              </Typography>
            </Grid>

            <Grid item xs={12} md={6}>
              <Typography variant="subtitle2" gutterBottom>
                General:
              </Typography>
              <Box display="flex" alignItems="center" justifyContent="flex-start">
                <Switch checked={nicDetails.autoConnect || false} />
                <Typography
                  variant="body1"
                  color="textSecondary"
                  style={{ marginLeft: "8px" }}
                >
                  Connect automatically
                </Typography>
              </Box>
            </Grid>

            <Grid item xs={12} md={6}>
              <Typography variant="subtitle2" gutterBottom>
                MTU:
              </Typography>
              <Typography variant="body1" color="textSecondary">
                Automatic{" "}
                <IconButton size="small">
                  <EditIcon fontSize="small" />
                </IconButton>
              </Typography>
            </Grid>
          </Grid>
        </CardContent>
      </Card>
    </Box>
  );
};

export default NetworkDetails;
