import CloseIcon from "@mui/icons-material/Close";
import { Drawer, Box, IconButton, Typography, Alert } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import axios from "@/utils/axios";

interface ServiceLogsDrawerProps {
  open: boolean;
  onClose: () => void;
  serviceName: string;
}

const ServiceLogsDrawer: React.FC<ServiceLogsDrawerProps> = ({
  open,
  onClose,
  serviceName,
}) => {
  const { data, isLoading, isError, error } = useQuery<string[]>({
    queryKey: ["service-logs", serviceName],
    queryFn: async () => {
      const res = await axios.get(`/services/${serviceName}/logs?lines=200`);
      return res.data;
    },
    enabled: open, // Only fetch when drawer is open
    refetchInterval: open ? 5000 : false, // Auto-refresh every 5 seconds when open
  });

  return (
    <Drawer anchor="right" open={open} onClose={onClose}>
      <Box
        sx={{
          width: 700,
          p: 3,
          height: "100%",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <Box
          display="flex"
          justifyContent="space-between"
          alignItems="center"
          mb={2}
        >
          <Typography variant="h6">Logs: {serviceName}</Typography>
          <IconButton onClick={onClose}>
            <CloseIcon />
          </IconButton>
        </Box>

        {isLoading && <ComponentLoader />}

        {isError && (
          <Alert severity="error">
            {error instanceof Error ? error.message : "Failed to load logs"}
          </Alert>
        )}

        {data && (
          <Box
            sx={{
              flex: 1,
              bgcolor: "#1e1e1e",
              color: "#d4d4d4",
              p: 2,
              borderRadius: 1,
              overflow: "auto",
              fontFamily: "monospace",
              fontSize: "0.85rem",
              whiteSpace: "pre-wrap",
              wordBreak: "break-all",
            }}
          >
            {data.map((line, idx) => (
              <div key={idx}>{line}</div>
            ))}
          </Box>
        )}
      </Box>
    </Drawer>
  );
};

export default ServiceLogsDrawer;
