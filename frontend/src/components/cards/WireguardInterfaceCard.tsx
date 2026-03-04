import AddIcon from "@mui/icons-material/Add";
import DeleteIcon from "@mui/icons-material/Delete";
import PowerSettingsNewIcon from "@mui/icons-material/PowerSettingsNew";
import RestartAltIcon from "@mui/icons-material/RestartAlt";
import {
  CardContent,
  Typography,
  Box,
  IconButton,
  Tooltip,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { motion } from "framer-motion";
import React, { RefObject, useState } from "react";

import FrostedCard from "@/components/cards/RootCard";
import {
  getAccentCardHoverStyles,
  getAccentCardStyles,
} from "@/theme/surfaces";
import { WireGuardInterface } from "@/types/wireguard";

// Props type
interface InterfaceCardProps {
  iface: WireGuardInterface;
  selectedInterface: string | null;
  selectedCardRef: RefObject<HTMLDivElement> | null;
  primaryColor?: string;
  handleSelectInterface: (iface: WireGuardInterface) => void;
  handleToggleInterface: (name: string, status: "up" | "down") => void;
  handleToggleBootPersistence: (name: string, isEnabled: boolean) => void;
  handleDelete: (name: string) => void;
  handleAddPeer: (name: string, peerData: any) => void;
}

const InterfaceCard: React.FC<InterfaceCardProps> = ({
  iface,
  selectedInterface,
  selectedCardRef,
  handleSelectInterface,
  handleToggleInterface,
  handleToggleBootPersistence,
  handleDelete,
  handleAddPeer,
}) => {
  const theme = useTheme();
  const color = "primary";
  const activeAccentColor =
    theme.palette[color]?.main || theme.palette.primary.main;
  const idleAccentColor =
    theme.palette[color]?.dark || theme.palette.primary.dark;

  const hoverStyles = getAccentCardHoverStyles(theme, activeAccentColor);
  const isSelected = iface.name === selectedInterface;
  const [hovered, setHovered] = useState(false);

  return (
    <motion.div
      initial={{ opacity: 0, y: -20 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 20 }}
      transition={{ duration: 0.3 }}
      layout
    >
      <FrostedCard
        ref={isSelected ? selectedCardRef : null}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          cursor: "pointer",
          ...getAccentCardStyles(idleAccentColor),
          transition:
            "border 0.3s ease-in-out, box-shadow 0.3s ease-in-out, margin 0.3s ease-in-out, transform 0.2s",
          ...((isSelected || hovered) && hoverStyles),
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
                        : theme.palette.text.disabled,
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
              <Tooltip
                title={
                  iface.isEnabled
                    ? "Disable Boot Persistence"
                    : "Enable Boot Persistence"
                }
              >
                <IconButton
                  sx={{
                    color: iface.isEnabled
                      ? theme.palette.success.main
                      : theme.palette.text.disabled,
                  }}
                  aria-label="Boot Persistence"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleToggleBootPersistence(iface.name, iface.isEnabled);
                  }}
                >
                  <RestartAltIcon />
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
              <Tooltip title="Delete Interface">
                <IconButton
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDelete(iface.name);
                  }}
                  sx={{ color: theme.palette.error.main }}
                >
                  <DeleteIcon />
                </IconButton>
              </Tooltip>
            </Box>
          </Box>
          <Typography variant="body2" color="text.secondary">
            Address: {iface.address}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Port: {iface.port}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Peers: {iface.peerCount}
          </Typography>
        </CardContent>
      </FrostedCard>
    </motion.div>
  );
};

export default InterfaceCard;
