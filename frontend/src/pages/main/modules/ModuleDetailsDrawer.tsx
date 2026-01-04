import CloseIcon from "@mui/icons-material/Close";
import {
  Drawer,
  Box,
  Typography,
  IconButton,
  Divider,
  Chip,
  List,
  ListItem,
  ListItemText,
  Alert,
  Link,
} from "@mui/material";
import React from "react";

import { linuxio } from "@/api/linuxio";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import type { ModuleDetailsInfo } from "@/types/module";

interface ModuleDetailsDrawerProps {
  open: boolean;
  onClose: () => void;
  moduleName: string | null;
}

const ModuleDetailsDrawer: React.FC<ModuleDetailsDrawerProps> = ({
  open,
  onClose,
  moduleName,
}) => {
  const {
    data: module,
    isPending,
    isError,
  } = linuxio.useCall<ModuleDetailsInfo>(
    "modules",
    "GetModuleDetails",
    moduleName ? [moduleName] : [],
    {
      enabled: open && !!moduleName,
    },
  );

  return (
    <Drawer anchor="right" open={open} onClose={onClose}>
      <Box sx={{ width: 500, p: 3 }}>
        {/* Header */}
        <Box sx={{ display: "flex", justifyContent: "space-between", mb: 2 }}>
          <Typography variant="h5">Module Details</Typography>
          <IconButton onClick={onClose}>
            <CloseIcon />
          </IconButton>
        </Box>

        <Divider sx={{ mb: 2 }} />

        {/* Content */}
        {isPending && <ComponentLoader />}

        {isError && (
          <Alert severity="error">Failed to load module details</Alert>
        )}

        {module && (
          <Box>
            <Typography variant="h6" gutterBottom>
              {module.title}
            </Typography>
            <Typography
              variant="body2"
              color="text.secondary"
              component="p"
              sx={{ mb: 2 }}
            >
              {module.description}
            </Typography>

            {/* Metadata */}
            <Box sx={{ mb: 3 }}>
              <Typography variant="subtitle2" gutterBottom>
                Information
              </Typography>
              <List dense>
                <ListItem>
                  <ListItemText primary="Name" secondary={module.name} />
                </ListItem>
                <ListItem>
                  <ListItemText primary="Version" secondary={module.version} />
                </ListItem>
                {module.author && (
                  <ListItem>
                    <ListItemText primary="Author" secondary={module.author} />
                  </ListItem>
                )}
                {module.license && (
                  <ListItem>
                    <ListItemText
                      primary="License"
                      secondary={module.license}
                    />
                  </ListItem>
                )}
                <ListItem>
                  <ListItemText primary="Path" secondary={module.path} />
                </ListItem>
                <ListItem>
                  <ListItemText
                    primary="Type"
                    secondary={
                      <Box sx={{ display: "flex", gap: 1, mt: 0.5 }}>
                        {module.isSystem && (
                          <Chip
                            label="System Module"
                            size="small"
                            color="primary"
                          />
                        )}
                        {module.isSymlink && (
                          <Chip
                            label="Symlink"
                            size="small"
                            variant="outlined"
                          />
                        )}
                        {!module.isSystem && !module.isSymlink && (
                          <Chip
                            label="User Module"
                            size="small"
                            variant="outlined"
                          />
                        )}
                      </Box>
                    }
                  />
                </ListItem>
              </List>
            </Box>

            {/* Handlers */}
            {module.handlers && module.handlers.length > 0 && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="subtitle2" gutterBottom>
                  Registered Handlers ({module.handlers.length})
                </Typography>
                <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
                  {module.handlers.map((handler) => (
                    <Chip key={handler} label={handler} size="small" />
                  ))}
                </Box>
              </Box>
            )}

            {/* Permissions */}
            {module.permissions && module.permissions.length > 0 && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="subtitle2" gutterBottom>
                  Required Permissions
                </Typography>
                <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
                  {module.permissions.map((perm) => (
                    <Chip
                      key={perm}
                      label={perm}
                      size="small"
                      color="warning"
                    />
                  ))}
                </Box>
              </Box>
            )}

            {/* Settings */}
            {module.settings && module.settings.length > 0 && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="subtitle2" gutterBottom>
                  Settings ({module.settings.length})
                </Typography>
                <List dense>
                  {module.settings.map((setting) => (
                    <ListItem key={setting.name}>
                      <ListItemText
                        primary={setting.name}
                        secondary={`${setting.type}: ${setting.description}`}
                      />
                    </ListItem>
                  ))}
                </List>
              </Box>
            )}

            {/* Homepage Link */}
            {module.homepage && (
              <Box>
                <Typography variant="subtitle2" gutterBottom>
                  Homepage
                </Typography>
                <Link
                  href={module.homepage}
                  target="_blank"
                  rel="noopener noreferrer"
                  sx={{ wordBreak: "break-all" }}
                >
                  {module.homepage}
                </Link>
              </Box>
            )}
          </Box>
        )}
      </Box>
    </Drawer>
  );
};

export default ModuleDetailsDrawer;
