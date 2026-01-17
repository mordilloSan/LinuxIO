import { Box, TableCell, TextField, Chip, Typography } from "@mui/material";
import React, { useCallback, useEffect, useState } from "react";

import linuxio from "@/api/react-query";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import {
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";

interface ImageListProps {
  onMountCreateHandler?: (handler: () => void) => void;
}

const ImageList: React.FC<ImageListProps> = ({ onMountCreateHandler }) => {
  const { data: images = [] } = linuxio.docker.list_images.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");

  // Create image handler
  const handleCreateImage = useCallback(() => {
    // TODO: Open image pull/import dialog
    console.log("Add image clicked");
  }, []);

  // Mount handler to parent
  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleCreateImage);
    }
  }, [onMountCreateHandler, handleCreateImage]);

  // Flatten images with multiple tags
  const imageRows = images.flatMap((img) => {
    const tags = img.RepoTags?.length ? img.RepoTags : ["<none>:<none>"];
    return tags.map((tag) => {
      const [repo, tagName] = tag.split(":");
      return {
        id: img.Id,
        repo: repo || "<none>",
        tag: tagName || "<none>",
        shortId: img.Id?.slice(7, 19) || "",
        size: (img.Size / (1024 * 1024)).toFixed(2),
        created: new Date(img.Created * 1000).toLocaleString(),
        containers: img.Containers || 0,
        raw: img,
      };
    });
  });

  const filtered = imageRows.filter(
    (img) =>
      img.repo.toLowerCase().includes(search.toLowerCase()) ||
      img.tag.toLowerCase().includes(search.toLowerCase()) ||
      img.shortId.toLowerCase().includes(search.toLowerCase()),
  );

  const columns: UnifiedTableColumn[] = [
    { field: "repo", headerName: "Repository", align: "left" },
    { field: "tag", headerName: "Tag", align: "left", width: "120px" },
    {
      field: "id",
      headerName: "Image ID",
      align: "left",
      width: "140px",
      sx: { display: { xs: "none", md: "table-cell" } },
    },
    { field: "size", headerName: "Size", align: "right", width: "100px" },
    {
      field: "created",
      headerName: "Created",
      align: "left",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
    { field: "usedBy", headerName: "Used By", align: "center", width: "100px" },
  ];

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2}>
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search images…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          sx={{
            width: 320,
            "@media (max-width: 600px)": {
              width: "100%",
            },
          }}
        />
        <Box fontWeight="bold">{filtered.length} shown</Box>
      </Box>
      <UnifiedCollapsibleTable
        data={filtered}
        columns={columns}
        getRowKey={(image) => `${image.id}-${image.tag}`}
        renderMainRow={(image) => (
          <>
            <TableCell>
              <Typography
                variant="body2"
                fontWeight="medium"
                sx={responsiveTextStyles}
              >
                {image.repo}
              </Typography>
            </TableCell>
            <TableCell>
              <Chip
                label={image.tag}
                size="small"
                sx={{ fontSize: "0.75rem" }}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Typography
                variant="body2"
                sx={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  ...responsiveTextStyles,
                }}
              >
                {image.shortId}
              </Typography>
            </TableCell>
            <TableCell align="right">
              <Typography variant="body2" sx={responsiveTextStyles}>
                {image.size} MB
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
              <Typography
                variant="body2"
                sx={{ fontSize: "0.85rem", ...responsiveTextStyles }}
              >
                {image.created}
              </Typography>
            </TableCell>
            <TableCell align="center">
              <Chip
                label={image.containers}
                size="small"
                color={image.containers > 0 ? "success" : "default"}
                sx={{ minWidth: 40 }}
              />
            </TableCell>
          </>
        )}
        renderExpandedContent={(image) => (
          <>
            <Typography variant="subtitle2" gutterBottom>
              <b>Full Image ID:</b>
            </Typography>
            <Typography
              variant="body2"
              sx={{
                fontFamily: "monospace",
                fontSize: "0.85rem",
                mb: 2,
                ...longTextStyles,
              }}
            >
              {image.id}
            </Typography>

            <Typography variant="subtitle2" gutterBottom>
              <b>Labels:</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {image.raw.Labels && Object.keys(image.raw.Labels).length > 0 ? (
                Object.entries(image.raw.Labels).map(([key, val]) => (
                  <Chip
                    key={key}
                    label={`${key}: ${val}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no labels)
                </Typography>
              )}
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              <b>Image Digests:</b>
            </Typography>
            <Box>
              {image.raw.RepoDigests && image.raw.RepoDigests.length > 0 ? (
                image.raw.RepoDigests.map((digest) => (
                  <Typography
                    key={digest}
                    variant="body2"
                    sx={{
                      fontFamily: "monospace",
                      fontSize: "0.8rem",
                      mb: 0.5,
                      ...longTextStyles,
                    }}
                  >
                    {digest}
                  </Typography>
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no digests)
                </Typography>
              )}
            </Box>
          </>
        )}
        emptyMessage="No images found."
      />
    </Box>
  );
};

export default ImageList;
