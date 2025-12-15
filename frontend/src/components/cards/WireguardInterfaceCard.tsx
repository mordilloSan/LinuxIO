import AddIcon from "@mui/icons-material/Add";
import DeleteIcon from "@mui/icons-material/Delete";
import PowerSettingsNewIcon from "@mui/icons-material/PowerSettingsNew";
import {
  Card,
  CardContent,
  Typography,
  Box,
  IconButton,
  Tooltip,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { motion } from "framer-motion";
import React, { RefObject } from "react";

import { WireGuardInterface } from "@/types/wireguard";

// Props type
interface InterfaceCardProps {
  iface: WireGuardInterface;
  selectedInterface: string | null;
  selectedCardRef: RefObject<HTMLDivElement> | null;
  primaryColor?: string;
  handleSelectInterface: (iface: WireGuardInterface) => void;
  handleToggleInterface: (name: string, status: "up" | "down") => void;
  handleDelete: (name: string) => void;
  handleAddPeer: (name: string, peerData: any) => void;
}

const InterfaceCard: React.FC<InterfaceCardProps> = ({
  iface,
  selectedInterface,
  selectedCardRef,
  handleSelectInterface,
  handleToggleInterface,
  handleDelete,
  handleAddPeer,
}) => {
  const theme = useTheme();
  const color = "primary";

  const hoverStyles = {
    borderBottomWidth: "3px",
    borderBottomColor: theme.palette[color]?.main || theme.palette.primary.main,
    boxShadow: theme.shadows[10],
    marginBlockEnd: "-1px",
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: -20 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 20 }}
      transition={{ duration: 0.3 }}
      layout
    >
      <Card
        ref={iface.name === selectedInterface ? selectedCardRef : null}
        sx={{
          cursor: "pointer",
          borderBottomWidth: "2px",
          borderBottomStyle: "solid",
          borderBottomColor:
            theme.palette[color]?.dark || theme.palette.primary.dark,
          transition:
            "border 0.3s ease-in-out, box-shadow 0.3s ease-in-out, margin 0.3s ease-in-out",
          "&:hover": hoverStyles,
          ...(iface.name === selectedInterface && hoverStyles),
        }}
        onClick={() => handleSelectInterface(iface)}
      >
        <CardContent>
          <Box
            display="flex"
            justifyContent="space-between"
            alignItems="center"
          >
            <Typography variant="h6" sx={{ fontSize: "1.1rem" }}>
              {iface.name}
            </Typography>
            <Box>
              <Tooltip
                title={iface.isConnected === "Active" ? "Turn Off" : "Turn On"}
              >
                <IconButton
                  sx={{
                    color:
                      iface.isConnected === "Active"
                        ? theme.palette.primary.light
                        : "gray",
                  }}
                  aria-label="Power"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleToggleInterface(
                      iface.name,
                      iface.isConnected === "Active" ? "down" : "up",
                    );
                  }}
                >
                  <PowerSettingsNewIcon />
                </IconButton>
              </Tooltip>
              <Tooltip title="Delete Interface">
                <IconButton
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDelete(iface.name);
                  }}
                  sx={{ color: "red" }}
                >
                  <DeleteIcon />
                </IconButton>
              </Tooltip>
              <Tooltip title="Add Peer">
                <IconButton
                  onClick={(e) => {
                    e.stopPropagation();
                    handleAddPeer(iface.name, {});
                  }}
                >
                  <AddIcon />
                </IconButton>
              </Tooltip>
            </Box>
          </Box>
          <Typography variant="body2" color="textSecondary">
            Address: {iface.address}
          </Typography>
          <Typography variant="body2" color="textSecondary">
            Port: {iface.port}
          </Typography>
          <Typography variant="body2" color="textSecondary">
            Peers: {iface.peerCount}
          </Typography>
        </CardContent>
      </Card>
    </motion.div>
  );
};

export default InterfaceCard;
