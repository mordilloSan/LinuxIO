import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";

import { linuxio } from "@/api";
import DockerImageCard from "@/components/cards/DockerImageCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
import AppSearchField from "@/components/ui/AppSearchField";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import {
  longTextStyles,
  responsiveTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";
interface ImageListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}
interface DeleteImageDialogProps {
  imageIds: string[];
  imageTags: string[];
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}
const DeleteImageDialog: React.FC<DeleteImageDialogProps> = ({
  open,
  onClose,
  imageIds,
  imageTags,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
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
          return {
            success: true,
            tag: imageTags[index],
          };
        } catch {
          return {
            success: false,
            tag: imageTags[index],
          };
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
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>
        Delete Image{imageIds.length > 1 ? "s" : ""}
      </AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the following image
          {imageIds.length > 1 ? "s" : ""}?
        </AppDialogContentText>
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            marginTop: theme.spacing(2),
            marginBottom: theme.spacing(1),
          }}
        >
          {imageTags.map((tag, idx) => (
            <Chip
              key={`${tag}-${idx}`}
              label={tag}
              size="small"
              style={{
                marginRight: 4,
                marginBottom: 4,
              }}
              variant="soft"
            />
          ))}
        </div>
        <AppDialogContentText
          style={{
            marginTop: 8,
            color: "var(--mui-palette-warning-main)",
          }}
        >
          This action cannot be undone. Images in use by containers cannot be
          deleted.
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isDeleting} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isDeleting}
          onClick={handleDelete}
          variant="contained"
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const ImageList: React.FC<ImageListProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const theme = useAppTheme();
  const { data: rawImages } = linuxio.docker.list_images.useQuery({
    refetchInterval: 10000,
  });
  const images = rawImages ?? [];
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
    {
      field: "repo",
      headerName: "Repository",
      align: "left",
    },
    {
      field: "tag",
      headerName: "Tag",
      align: "left",
      width: "120px",
    },
    {
      field: "id",
      headerName: "Image ID",
      align: "left",
      width: "140px",
      className: "app-table-hide-below-md",
    },
    {
      field: "size",
      headerName: "Size",
      align: "right",
      width: "100px",
    },
    {
      field: "created",
      headerName: "Created",
      align: "left",
      className: "app-table-hide-below-sm",
    },
    {
      field: "usedBy",
      headerName: "Used By",
      align: "center",
      width: "100px",
    },
  ];
  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(2),
          flexWrap: "wrap",
          marginBottom: theme.spacing(2),
        }}
      >
        <AppSearchField
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search images…"
          style={{ width: 320 }}
          value={search}
        />
        <AppTypography fontWeight={700}>{filtered.length} shown</AppTypography>
        {effectiveSelected.size > 0 && (
          <AppButton
            color="error"
            onClick={() => setDeleteDialogOpen(true)}
            size="small"
            startIcon={<Icon height={20} icon="mdi:delete" width={20} />}
            variant="contained"
          >
            Delete ({effectiveSelected.size})
          </AppButton>
        )}
      </div>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <AppGrid container spacing={2}>
            {filtered.map((image) => (
              <AppGrid
                key={`${image.id}-${image.tag}`}
                size={{
                  xs: 12,
                  sm: 6,
                  md: 4,
                  lg: 3,
                }}
              >
                <DockerImageCard
                  image={image}
                  onSelect={(checked) => handleSelectOne(image.id, checked)}
                  selected={effectiveSelected.has(image.id)}
                />
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingTop: theme.spacing(4),
              paddingBottom: theme.spacing(4),
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No images found.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          columns={columns}
          data={filtered}
          emptyMessage="No images found."
          getRowKey={(image) => `${image.id}-${image.tag}`}
          renderExpandedContent={(image) => (
            <>
              <AppTypography gutterBottom variant="subtitle2">
                <b>Full Image ID:</b>
              </AppTypography>
              <AppTypography
                style={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  marginBottom: 8,
                  ...longTextStyles,
                }}
                variant="body2"
              >
                {image.id}
              </AppTypography>

              <AppTypography gutterBottom variant="subtitle2">
                <b>Labels:</b>
              </AppTypography>
              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  marginBottom: theme.spacing(2),
                }}
              >
                {image.raw.Labels &&
                Object.keys(image.raw.Labels).length > 0 ? (
                  Object.entries(image.raw.Labels).map(([key, val]) => (
                    <Chip
                      key={key}
                      label={`${key}: ${val}`}
                      size="small"
                      sx={{
                        mr: 1,
                        mb: 1,
                        ...wrappableChipStyles,
                      }}
                      variant="soft"
                    />
                  ))
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    (no labels)
                  </AppTypography>
                )}
              </div>

              <AppTypography gutterBottom variant="subtitle2">
                <b>Image Digests:</b>
              </AppTypography>
              <div>
                {image.raw.RepoDigests && image.raw.RepoDigests.length > 0 ? (
                  image.raw.RepoDigests.map((digest) => (
                    <AppTypography
                      key={digest}
                      style={{
                        fontFamily: "monospace",
                        fontSize: "0.8rem",
                        marginBottom: 2,
                        ...longTextStyles,
                      }}
                      variant="body2"
                    >
                      {digest}
                    </AppTypography>
                  ))
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    (no digests)
                  </AppTypography>
                )}
              </div>
            </>
          )}
          renderFirstCell={(image) => (
            <AppCheckbox
              checked={effectiveSelected.has(image.id)}
              onChange={(e) => handleSelectOne(image.id, e.target.checked)}
              onClick={(e) => e.stopPropagation()}
              size="small"
            />
          )}
          renderHeaderFirstCell={() => (
            <AppCheckbox
              checked={allSelected}
              indeterminate={someSelected}
              onChange={(e) => handleSelectAll(e.target.checked)}
              size="small"
            />
          )}
          renderMainRow={(image) => (
            <>
              <AppTableCell>
                <AppTypography
                  fontWeight={500}
                  style={responsiveTextStyles}
                  variant="body2"
                >
                  {image.repo}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <Chip
                  label={image.tag}
                  size="small"
                  style={{
                    fontSize: "0.75rem",
                  }}
                  variant="soft"
                />
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                <AppTypography
                  style={{
                    fontFamily: "monospace",
                    fontSize: "0.85rem",
                    ...responsiveTextStyles,
                  }}
                  variant="body2"
                >
                  {image.shortId}
                </AppTypography>
              </AppTableCell>
              <AppTableCell align="right">
                <AppTypography style={responsiveTextStyles} variant="body2">
                  {image.size} MB
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <AppTypography
                  style={{
                    fontSize: "0.85rem",
                    ...responsiveTextStyles,
                  }}
                  variant="body2"
                >
                  {image.created}
                </AppTypography>
              </AppTableCell>
              <AppTableCell align="center">
                <Chip
                  color={image.containers > 0 ? "success" : "default"}
                  label={image.containers}
                  size="small"
                  style={{
                    minWidth: 40,
                  }}
                  variant="soft"
                />
              </AppTableCell>
            </>
          )}
        />
      )}

      <DeleteImageDialog
        imageIds={selectedImages.map((img) => img.id)}
        imageTags={selectedImages.map((img) => `${img.repo}:${img.tag}`)}
        onClose={() => setDeleteDialogOpen(false)}
        onSuccess={handleDeleteSuccess}
        open={deleteDialogOpen}
      />
    </div>
  );
};
export default ImageList;
