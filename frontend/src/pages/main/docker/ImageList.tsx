import DeleteIcon from "@mui/icons-material/Delete";
import {
  Box,
  TableCell,
  TextField,
  Chip,
  Typography,
  Checkbox,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
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

interface DeleteImageDialogProps {
  open: boolean;
  onClose: () => void;
  imageIds: string[];
  imageTags: string[];
  onSuccess: () => void;
}

const DeleteImageDialog: React.FC<DeleteImageDialogProps> = ({
  open,
  onClose,
  imageIds,
  imageTags,
  onSuccess,
}) => {
  const queryClient = useQueryClient();

  const { mutateAsync: deleteImage, isPending: isDeleting } =
    linuxio.docker.delete_image.useMutation({
      onError: () => {
        // Suppress global error handler - errors handled manually in handleDelete
      },
    });

  const handleDelete = async () => {
    // Delete images sequentially, tracking successes and failures
    const results = await Promise.all(
      imageIds.map(async (id, index) => {
        try {
          await deleteImage([id]);
          return { success: true, tag: imageTags[index] };
        } catch {
          return { success: false, tag: imageTags[index] };
        }
      }),
    );

    const succeeded = results.filter((r) => r.success);
    const failed = results.filter((r) => !r.success);

    if (succeeded.length > 0) {
      const successMessage =
        succeeded.length === 1
          ? `Image "${succeeded[0].tag}" deleted successfully`
          : `${succeeded.length} images deleted successfully`;
      toast.success(successMessage);
    }

    if (failed.length > 0) {
      const failMessage =
        failed.length === 1
          ? `Could not delete "${failed[0].tag}" (likely in use)`
          : `Could not delete ${failed.length} images (likely in use)`;
      toast.error(failMessage);
    }

    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_images.queryKey(),
    });
    onSuccess();
    handleClose();
  };

  const handleClose = () => {
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Delete Image{imageIds.length > 1 ? "s" : ""}</DialogTitle>
      <DialogContent>
        <DialogContentText>
          Are you sure you want to delete the following image
          {imageIds.length > 1 ? "s" : ""}?
        </DialogContentText>
        <Box sx={{ mt: 2, mb: 1 }}>
          {imageTags.map((tag, idx) => (
            <Chip
              key={`${tag}-${idx}`}
              label={tag}
              size="small"
              sx={{ mr: 1, mb: 1 }}
            />
          ))}
        </Box>
        <DialogContentText sx={{ mt: 2, color: "warning.main" }}>
          This action cannot be undone. Images in use by containers cannot be
          deleted.
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={isDeleting}>
          Cancel
        </Button>
        <Button
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const ImageList: React.FC<ImageListProps> = ({ onMountCreateHandler }) => {
  const { data: images = [] } = linuxio.docker.list_images.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

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

  // Compute effective selection - only include items that are in the filtered list
  const effectiveSelected = useMemo(() => {
    const filteredIds = new Set(filtered.map((img) => img.id));
    const result = new Set<string>();
    selected.forEach((id) => {
      if (filteredIds.has(id)) {
        result.add(id);
      }
    });
    return result;
  }, [selected, filtered]);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelected(new Set(filtered.map((img) => img.id)));
    } else {
      setSelected(new Set());
    }
  };

  const handleSelectOne = (id: string, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(id);
      } else {
        next.delete(id);
      }
      return next;
    });
  };

  const handleDeleteSuccess = () => {
    setSelected(new Set());
  };

  const selectedImages = filtered.filter((img) =>
    effectiveSelected.has(img.id),
  );
  const allSelected =
    filtered.length > 0 && effectiveSelected.size === filtered.length;
  const someSelected =
    effectiveSelected.size > 0 && effectiveSelected.size < filtered.length;

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
      <Box mb={2} display="flex" alignItems="center" gap={2} flexWrap="wrap">
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
        {effectiveSelected.size > 0 && (
          <Button
            variant="contained"
            color="error"
            size="small"
            startIcon={<DeleteIcon />}
            onClick={() => setDeleteDialogOpen(true)}
          >
            Delete ({effectiveSelected.size})
          </Button>
        )}
      </Box>
      <UnifiedCollapsibleTable
        data={filtered}
        columns={columns}
        getRowKey={(image) => `${image.id}-${image.tag}`}
        renderFirstCell={(image) => (
          <Checkbox
            size="small"
            checked={effectiveSelected.has(image.id)}
            onChange={(e) => handleSelectOne(image.id, e.target.checked)}
            onClick={(e) => e.stopPropagation()}
          />
        )}
        renderHeaderFirstCell={() => (
          <Checkbox
            size="small"
            checked={allSelected}
            indeterminate={someSelected}
            onChange={(e) => handleSelectAll(e.target.checked)}
          />
        )}
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

      <DeleteImageDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        imageIds={selectedImages.map((img) => img.id)}
        imageTags={selectedImages.map((img) => `${img.repo}:${img.tag}`)}
        onSuccess={handleDeleteSuccess}
      />
    </Box>
  );
};

export default ImageList;
