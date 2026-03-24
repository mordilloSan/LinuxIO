import { Icon } from "@iconify/react";
import React from "react";
import { createPortal } from "react-dom";

import linuxio from "@/api/react-query";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert from "@/components/ui/AppAlert";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppIconButton from "@/components/ui/AppIconButton";

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

  if (!open) {
    return null;
  }

  return createPortal(
    <div style={{ position: "fixed", inset: 0, zIndex: 1350 }}>
      <div
        aria-hidden
        onClick={onClose}
        style={{
          position: "absolute",
          inset: 0,
          background: "rgba(0, 0, 0, 0.45)",
          backdropFilter: "blur(6px)",
        }}
      />
      <div
        role="dialog"
        aria-modal="true"
        style={{
          position: "absolute",
          top: 0,
          right: 0,
          height: "100%",
          width: "min(500px, calc(100vw - 16px))",
          padding: 24,
          overflowY: "auto",
          background: "var(--app-palette-background-paper)",
          color: "var(--app-palette-text-primary)",
          boxShadow: "var(--app-panel-shadow)",
          borderLeft: "1px solid var(--app-palette-divider)",
        }}
      >
        {/* Header */}
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            marginBottom: 8,
          }}
        >
          <h2 style={{ margin: 0, fontSize: "1.5rem", fontWeight: 600 }}>
            Module Details
          </h2>
          <AppIconButton onClick={onClose}>
            <Icon icon="mdi:close" width={20} height={20} />
          </AppIconButton>
        </div>

        <AppDivider style={{ marginBottom: 8 }} />

        {/* Content */}
        {isPending && <ComponentLoader />}

        {isError && (
          <AppAlert severity="error">Failed to load module details</AppAlert>
        )}

        {module && (
          <div>
            <h3 style={{ margin: "0 0 8px", fontSize: "1.125rem" }}>
              {module.title}
            </h3>
            <p
              style={{
                margin: "0 0 8px",
                color: "var(--app-palette-text-secondary)",
                fontSize: "0.875rem",
                lineHeight: 1.45,
              }}
            >
              {module.description}
            </p>

            {/* Metadata */}
            <div style={{ marginBottom: 12 }}>
              <p
                style={{
                  margin: "0 0 8px",
                  fontSize: "0.875rem",
                  fontWeight: 600,
                }}
              >
                Information
              </p>
              <div style={{ display: "grid", gap: 10 }}>
                <div>
                  <strong>Name</strong>
                  <div>{module.name}</div>
                </div>
                <div>
                  <strong>Version</strong>
                  <div>{module.version}</div>
                </div>
                {module.author && (
                  <div>
                    <strong>Author</strong>
                    <div>{module.author}</div>
                  </div>
                )}
                {module.license && (
                  <div>
                    <strong>License</strong>
                    <div>{module.license}</div>
                  </div>
                )}
                <div>
                  <strong>Path</strong>
                  <div style={{ wordBreak: "break-all" }}>{module.path}</div>
                </div>
                <div>
                  <strong>Type</strong>
                  <div style={{ display: "flex", gap: 4, marginTop: 6 }}>
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
                      <Chip label="User Module" size="small" variant="soft" />
                    )}
                  </div>
                </div>
              </div>
            </div>

            {/* Handlers */}
            {module.handlers && module.handlers.length > 0 && (
              <div style={{ marginBottom: 12 }}>
                <p
                  style={{
                    margin: "0 0 8px",
                    fontSize: "0.875rem",
                    fontWeight: 600,
                  }}
                >
                  Registered Handlers ({module.handlers.length})
                </p>
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
                <p
                  style={{
                    margin: "0 0 8px",
                    fontSize: "0.875rem",
                    fontWeight: 600,
                  }}
                >
                  Required Permissions
                </p>
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
                <p
                  style={{
                    margin: "0 0 8px",
                    fontSize: "0.875rem",
                    fontWeight: 600,
                  }}
                >
                  Settings ({module.settings.length})
                </p>
                <div style={{ display: "grid", gap: 10 }}>
                  {module.settings.map((setting) => (
                    <div key={setting.name}>
                      <strong>{setting.name}</strong>
                      <div>{`${setting.type}: ${setting.description}`}</div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Homepage Link */}
            {module.homepage && (
              <div>
                <p
                  style={{
                    margin: "0 0 8px",
                    fontSize: "0.875rem",
                    fontWeight: 600,
                  }}
                >
                  Homepage
                </p>
                <a
                  href={module.homepage}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{
                    color: "var(--app-palette-primary-main)",
                    wordBreak: "break-all",
                  }}
                >
                  {module.homepage}
                </a>
              </div>
            )}
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
};

export default ModuleDetailsDrawer;
