import { Icon } from "@iconify/react";
import { useRef, useState } from "react";

import type { NFSClient } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";

/** The export options every client of one NFS export shares. */
export interface ClientOptions {
  allSquash: boolean;
  crossmnt: boolean;
  insecure: boolean;
  noRootSquash: boolean;
  noSubtreeCheck: boolean;
  rw: boolean;
  sync: boolean;
}

export const defaultNFSOptions: ClientOptions = {
  rw: true,
  sync: true,
  noSubtreeCheck: true,
  noRootSquash: false,
  allSquash: false,
  insecure: false,
  crossmnt: false,
};

const nfsOptionLabels: { key: keyof ClientOptions; label: string }[] = [
  { key: "rw", label: "Read / Write" },
  { key: "sync", label: "Sync" },
  { key: "noSubtreeCheck", label: "No Subtree Check" },
  { key: "noRootSquash", label: "No Root Squash" },
  { key: "allSquash", label: "All Squash" },
  { key: "insecure", label: "Insecure" },
  { key: "crossmnt", label: "Crossmnt" },
];

function nfsOptionsToStrings(options: ClientOptions): string[] {
  const values: string[] = [];
  values.push(options.rw ? "rw" : "ro");
  values.push(options.sync ? "sync" : "async");
  values.push(options.noSubtreeCheck ? "no_subtree_check" : "subtree_check");
  if (options.allSquash) {
    values.push("all_squash");
  } else {
    values.push(options.noRootSquash ? "no_root_squash" : "root_squash");
  }
  if (options.insecure) {
    values.push("insecure");
  }
  if (options.crossmnt) {
    values.push("crossmnt");
  }
  return values;
}

function nfsOptionsSummary(options: ClientOptions): string {
  return nfsOptionsToStrings(options).join(", ");
}

export function nfsOptionsFromStrings(options: string[] = []): ClientOptions {
  const set = new Set(options);
  return {
    rw: !set.has("ro"),
    sync: !set.has("async"),
    noSubtreeCheck: !set.has("subtree_check"),
    noRootSquash: set.has("no_root_squash"),
    allSquash: set.has("all_squash"),
    insecure: set.has("insecure"),
    crossmnt: set.has("crossmnt"),
  };
}

export function parseNFSClients(
  value: string,
  options: ClientOptions,
): NFSClient[] {
  return value
    .split(",")
    .map((client) => client.trim())
    .filter(Boolean)
    .map((host) => ({
      host,
      options: nfsOptionsToStrings(options),
    }));
}

/** The options of an NFS export as a field that opens their list. */
const NFSOptionsDropdown = ({
  options,
  onChange,
}: {
  options: ClientOptions;
  onChange: (next: ClientOptions) => void;
}) => {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const toggle = (key: keyof ClientOptions) =>
    onChange({ ...options, [key]: !options[key] });

  return (
    <>
      <div ref={anchorRef} style={{ flex: 1, minWidth: 220 }}>
        <AppTextField
          endAdornment={
            <Icon
              icon={open ? "mdi:chevron-up" : "mdi:chevron-down"}
              style={{ opacity: 0.5 }}
              width={18}
            />
          }
          fullWidth
          label="Options"
          onClick={() => {
            setAnchorEl(anchorRef.current);
            setOpen(true);
          }}
          size="small"
          style={{ cursor: "pointer" }}
          value={nfsOptionsSummary(options)}
        />
      </div>
      <AppPopover
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        matchAnchorWidth
        onClose={() => setOpen(false)}
        open={open}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
      >
        <div style={{ padding: "6px 0" }}>
          {nfsOptionLabels.map(({ key, label }) => (
            <AppButton
              color="inherit"
              key={key}
              onClick={() => toggle(key)}
              fullWidth
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "flex-start",
                gap: 10,
                width: "100%",
                padding: "7px 14px",
                background: "none",
                border: "none",
                cursor: "pointer",
                color: "inherit",
                textAlign: "left",
              }}
            >
              <span
                aria-hidden="true"
                style={{
                  width: 10,
                  height: 10,
                  borderRadius: "50%",
                  backgroundColor: options[key]
                    ? "var(--app-palette-success-main)"
                    : "var(--app-palette-text-disabled)",
                  flexShrink: 0,
                  transition: "background-color 150ms ease",
                }}
              />
              <AppTypography component="span" variant="body2">
                {label}
              </AppTypography>
            </AppButton>
          ))}
        </div>
      </AppPopover>
    </>
  );
};

export default NFSOptionsDropdown;
