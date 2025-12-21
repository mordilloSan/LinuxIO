import { Box, Typography, Chip } from "@mui/material";
import { useState } from "react";

import CollapsibleCard from "./DockerImageCard"; // ← new import

import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useStreamQuery } from "@/hooks/useStreamApi";
import { CollapsibleColumn } from "@/types/collapsible";

const formatImageRows = (images: any[]) =>
  images.flatMap((img) =>
    (img.RepoTags?.length ? img.RepoTags : ["<none>:<none>"]).map(
      (tag: string) => {
        const [repo, tagName] = tag.split(":");
        return {
          repo,
          tag: tagName,
          id: img.Id?.slice(7, 19),
          size: (img.Size / (1024 * 1024)).toFixed(2) + " MB",
          created: new Date(img.Created * 1000).toLocaleString(),
          containers: img.Containers,
          raw: img,
        };
      },
    ),
  );

const imageColumns: CollapsibleColumn[] = [
  { field: "repo", headerName: "Repository", align: "left" },
  { field: "tag", headerName: "Tag", align: "left" },
  { field: "id", headerName: "Image ID", align: "left" },
  { field: "size", headerName: "Size", align: "right" },
  { field: "created", headerName: "Created", align: "left" },
  { field: "containers", headerName: "Used By", align: "right" },
];

function renderCollapseContent(row: any) {
  const img = row.raw as any;

  const labelEntries =
    img && img.Labels
      ? Object.entries(img.Labels as Record<string, unknown>)
      : [];
  const hasLabels = labelEntries.length > 0;

  const digests: string[] = Array.isArray(img?.RepoDigests)
    ? img.RepoDigests
    : [];
  const hasDigests = digests.length > 0;

  return (
    <Box>
      <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
        Labels:
      </Typography>
      <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
        {hasLabels ? (
          <>
            {labelEntries.map(([key, val]) => {
              const label = `${key}: ${String(val)}`;
              return (
                <Chip
                  key={label}
                  label={label}
                  size="small"
                  sx={{
                    mr: 1,
                    mb: 1,
                    fontSize: { xs: "0.75rem", sm: "0.875rem" },
                    height: { xs: 22, sm: 26 },
                  }}
                />
              );
            })}
          </>
        ) : (
          <Typography variant="body2" color="text.secondary">
            (no labels)
          </Typography>
        )}
      </Box>

      <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
        Image Digests:
      </Typography>
      <Box sx={{ display: "flex", flexWrap: "wrap" }}>
        {hasDigests ? (
          <>
            {digests.map((digest) => (
              <Typography
                variant="body2"
                key={digest}
                sx={{
                  mr: 2,
                  mb: 0.5,
                  wordBreak: "break-all",
                  fontSize: { xs: "0.8rem", sm: "1rem" },
                }}
              >
                {digest}
              </Typography>
            ))}
          </>
        ) : (
          <Typography variant="body2" color="text.secondary">
            (no digests)
          </Typography>
        )}
      </Box>
    </Box>
  );
}

export default function ImageList() {
  const { data = [], isPending: isLoading } = useStreamQuery<any[]>({
    handlerType: "docker",
    command: "list_images",
  });

  const rows = formatImageRows(data);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  if (isLoading) {
    return (
      <Box
        sx={{ width: "100%", mt: 4, display: "flex", justifyContent: "center" }}
      >
        <ComponentLoader />
      </Box>
    );
  }

  return (
    <Box
      sx={{
        width: "100%",
        maxWidth: "var(--page-content-width, 100%)",
        mx: "auto",
        mt: 2,
        px: { xs: 2, md: 3 },
        boxSizing: "border-box",
        position: "relative",
      }}
    >
      {rows.map((row, idx) => {
        const key = String(row.id ?? idx);
        const isSelected = selected.has(key);
        return (
          <CollapsibleCard
            key={key}
            row={row}
            columns={imageColumns}
            renderCollapseContent={renderCollapseContent}
            selected={isSelected}
            onToggleSelected={() =>
              setSelected((prev) => {
                const next = new Set(prev);
                if (next.has(key)) {
                  next.delete(key);
                } else {
                  next.add(key);
                }
                return next;
              })
            }
          />
        );
      })}
    </Box>
  );
}
