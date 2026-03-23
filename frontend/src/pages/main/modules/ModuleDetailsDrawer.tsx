import { Icon } from "@iconify/react";
import {
  Drawer,
  Typography,
  IconButton,
  Divider,
  List,
  ListItem,
  ListItemText,
  Alert,
  Link,
} from "@mui/material";
import React from "react";

import linuxio from "@/api/react-query";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import Chip from "@/components/ui/AppChip";

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
  } = linuxio.modules.get_module_details.useQuery(moduleName ?? "", {
    enabled: open && !!moduleName,
  });

  return (
    <Drawer anchor="right" open={open} onClose={onClose}>
      <div style={{ width: 500, padding: 24 }}>
        {/* Header */}
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            marginBottom: 8,
          }}
        >
          <Typography variant="h5">Module Details</Typography>
          <IconButton onClick={onClose}>
            <Icon icon="mdi:close" width={20} height={20} />
          </IconButton>
        </div>

        <Divider style={{ marginBottom: 8 }} />

        {/* Content */}
        {isPending && <ComponentLoader />}

        {isError && (
          <Alert severity="error">Failed to load module details</Alert>
        )}

        {module && (
          <div>
            <Typography variant="h6" gutterBottom>
              {module.title}
            </Typography>
            <Typography
              variant="body2"
              color="text.secondary"
              component="p"
              style={{ marginBottom: 8 }}
            >
              {module.description}
            </Typography>

            {/* Metadata */}
            <div style={{ marginBottom: 12 }}>
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
                      <div style={{ display: "flex", gap: 4, marginTop: 2 }}>
                        {module.isSystem && (
                          <Chip
                            label="System Module"
                            size="small"
                            color="primary"
                            variant="soft"
                          />
                        )}
                        {module.isSymlink && (
                          <Chip label="Symlink" size="small" variant="soft" />
                        )}
                        {!module.isSystem && !module.isSymlink && (
                          <Chip
                            label="User Module"
                            size="small"
                            variant="soft"
                          />
                        )}
                      </div>
                    }
                  />
                </ListItem>
              </List>
            </div>

            {/* Handlers */}
            {module.handlers && module.handlers.length > 0 && (
              <div style={{ marginBottom: 12 }}>
                <Typography variant="subtitle2" gutterBottom>
                  Registered Handlers ({module.handlers.length})
                </Typography>
                <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                  {module.handlers.map((handler) => (
                    <Chip
                      key={handler}
                      label={handler}
                      size="small"
                      variant="soft"
                    />
                  ))}
                </div>
              </div>
            )}

            {/* Permissions */}
            {module.permissions && module.permissions.length > 0 && (
              <div style={{ marginBottom: 12 }}>
                <Typography variant="subtitle2" gutterBottom>
                  Required Permissions
                </Typography>
                <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                  {module.permissions.map((perm) => (
                    <Chip
                      key={perm}
                      label={perm}
                      size="small"
                      variant="soft"
                      color="warning"
                    />
                  ))}
                </div>
              </div>
            )}

            {/* Settings */}
            {module.settings && module.settings.length > 0 && (
              <div style={{ marginBottom: 12 }}>
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
              </div>
            )}

            {/* Homepage Link */}
            {module.homepage && (
              <div>
                <Typography variant="subtitle2" gutterBottom>
                  Homepage
                </Typography>
                <Link
                  href={module.homepage}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{ wordBreak: "break-all" }}
                >
                  {module.homepage}
                </Link>
              </div>
            )}
          </div>
        )}
      </div>
    </Drawer>
  );
};

export default ModuleDetailsDrawer;
