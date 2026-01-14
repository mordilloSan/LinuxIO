import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  TextField,
  Collapse,
  Chip,
  Typography,
} from "@mui/material";
import { motion } from "framer-motion";
import React, { useState } from "react";

import type { DockerImage } from "@/api/linuxio-types";
import linuxio from "@/api/react-query";
import {
  getTableHeaderStyles,
  getTableRowStyles,
  getExpandedRowStyles,
  getExpandedContentStyles,
  tableContainerStyles,
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/styles/tableStyles";

const ImageList: React.FC = () => {
  const { data: images = [] } = linuxio.docker.list_images.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

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
      <TableContainer sx={tableContainerStyles}>
        <Table size="small" sx={{ borderRadius: 3, boxShadow: 2 }}>
          <TableHead>
            <TableRow sx={getTableHeaderStyles}>
              <TableCell>Repository</TableCell>
              <TableCell>Tag</TableCell>
              <TableCell>Image ID</TableCell>
              <TableCell align="right">Size</TableCell>
              <TableCell>Created</TableCell>
              <TableCell align="center">Used By</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {filtered.map((image, index) => {
              const rowKey = `${image.id}-${image.tag}`;
              const rowStyles = (theme: any) => getTableRowStyles(theme, index);
              const expandedRowStyles = (theme: any) =>
                getExpandedRowStyles(theme, index);
              return (
                <React.Fragment key={rowKey}>
                  <TableRow sx={rowStyles}>
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
                    <TableCell>
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
                    <TableCell>
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
                    <TableCell>
                      <IconButton
                        size="small"
                        onClick={() =>
                          setExpanded(expanded === rowKey ? null : rowKey)
                        }
                      >
                        <ExpandMoreIcon
                          style={{
                            transform:
                              expanded === rowKey
                                ? "rotate(180deg)"
                                : "rotate(0deg)",
                            transition: "0.2s",
                          }}
                        />
                      </IconButton>
                    </TableCell>
                  </TableRow>
                  <TableRow sx={expandedRowStyles}>
                    <TableCell
                      style={{ paddingBottom: 0, paddingTop: 0 }}
                      colSpan={7}
                    >
                      <Collapse
                        in={expanded === rowKey}
                        timeout="auto"
                        unmountOnExit
                      >
                        <Box
                          component={motion.div}
                          initial={{ opacity: 0, y: -10 }}
                          animate={{ opacity: 1, y: 0 }}
                          sx={getExpandedContentStyles}
                        >
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
                          <Box
                            sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}
                          >
                            {image.raw.Labels &&
                            Object.keys(image.raw.Labels).length > 0 ? (
                              Object.entries(image.raw.Labels).map(
                                ([key, val]) => (
                                  <Chip
                                    key={key}
                                    label={`${key}: ${val}`}
                                    size="small"
                                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                                  />
                                ),
                              )
                            ) : (
                              <Typography
                                variant="body2"
                                color="text.secondary"
                              >
                                (no labels)
                              </Typography>
                            )}
                          </Box>

                          <Typography variant="subtitle2" gutterBottom>
                            <b>Image Digests:</b>
                          </Typography>
                          <Box>
                            {image.raw.RepoDigests &&
                            image.raw.RepoDigests.length > 0 ? (
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
                              <Typography
                                variant="body2"
                                color="text.secondary"
                              >
                                (no digests)
                              </Typography>
                            )}
                          </Box>
                        </Box>
                      </Collapse>
                    </TableCell>
                  </TableRow>
                </React.Fragment>
              );
            })}
          </TableBody>
        </Table>
      </TableContainer>
      {filtered.length === 0 && (
        <Box textAlign="center" py={4}>
          <Typography variant="body2" color="text.secondary">
            No images found.
          </Typography>
        </Box>
      )}
    </Box>
  );
};

export default ImageList;
