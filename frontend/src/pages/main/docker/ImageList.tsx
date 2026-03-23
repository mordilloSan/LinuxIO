import { Icon } from "@iconify/react";
import { useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
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
import {
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";
interface ImageListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
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
  const theme = useTheme();
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
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
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
              variant="soft"
              style={{
                marginRight: 4,
                marginBottom: 4,
              }}
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
        <AppButton onClick={handleClose} disabled={isDeleting}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting}
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
  const theme = useTheme();
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
          placeholder="Search images…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{ width: 320 }}
        />
        <AppTypography fontWeight={700}>{filtered.length} shown</AppTypography>
        {effectiveSelected.size > 0 && (
          <AppButton
            variant="contained"
            color="error"
            size="small"
            startIcon={<Icon icon="mdi:delete" width={20} height={20} />}
            onClick={() => setDeleteDialogOpen(true)}
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
                <FrostedCard
                  style={{
                    padding: 8,
                  }}
                >
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      gap: theme.spacing(1),
                      marginBottom: theme.spacing(1),
                    }}
                  >
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: theme.spacing(1),
                      }}
                    >
                      <AppCheckbox
                        size="small"
                        checked={effectiveSelected.has(image.id)}
                        onChange={(e) =>
                          handleSelectOne(image.id, e.target.checked)
                        }
                      />
                      <AppTypography variant="body2" fontWeight={700} noWrap>
                        {image.repo}
                      </AppTypography>
                    </div>
                    <Chip
                      label={image.tag}
                      size="small"
                      variant="soft"
                      style={{
                        fontSize: "0.75rem",
                      }}
                    />
                  </div>

                  <div
                    style={{
                      display: "grid",
                      gap: theme.spacing(0.5),
                      marginBottom: theme.spacing(1.5),
                    }}
                  >
                    <AppTypography variant="body2" style={responsiveTextStyles}>
                      Size: {image.size} MB
                    </AppTypography>
                    <AppTypography
                      variant="body2"
                      style={{
                        fontFamily: "monospace",
                        ...responsiveTextStyles,
                      }}
                    >
                      ID: {image.shortId}
                    </AppTypography>
                    <AppTypography
                      variant="body2"
                      style={{
                        fontSize: "0.82rem",
                        ...responsiveTextStyles,
                      }}
                    >
                      Created: {image.created}
                    </AppTypography>
                  </div>

                  <div
                    style={{
                      display: "flex",
                      gap: theme.spacing(1),
                      marginBottom: theme.spacing(1.5),
                    }}
                  >
                    <Chip
                      label={`Used by ${image.containers}`}
                      size="small"
                      variant="soft"
                      color={image.containers > 0 ? "success" : "default"}
                    />
                  </div>

                  <AppTypography variant="caption" color="text.secondary">
                    Full ID
                  </AppTypography>
                  <AppTypography
                    variant="body2"
                    style={{
                      fontFamily: "monospace",
                      fontSize: "0.75rem",
                      marginBottom: 4,
                      ...longTextStyles,
                    }}
                  >
                    {image.id}
                  </AppTypography>
                </FrostedCard>
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
            <AppTypography variant="body2" color="text.secondary">
              No images found.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          data={filtered}
          columns={columns}
          getRowKey={(image) => `${image.id}-${image.tag}`}
          renderFirstCell={(image) => (
            <AppCheckbox
              size="small"
              checked={effectiveSelected.has(image.id)}
              onChange={(e) => handleSelectOne(image.id, e.target.checked)}
              onClick={(e) => e.stopPropagation()}
            />
          )}
          renderHeaderFirstCell={() => (
            <AppCheckbox
              size="small"
              checked={allSelected}
              indeterminate={someSelected}
              onChange={(e) => handleSelectAll(e.target.checked)}
            />
          )}
          renderMainRow={(image) => (
            <>
              <AppTableCell>
                <AppTypography
                  variant="body2"
                  fontWeight={500}
                  style={responsiveTextStyles}
                >
                  {image.repo}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <Chip
                  label={image.tag}
                  size="small"
                  variant="soft"
                  style={{
                    fontSize: "0.75rem",
                  }}
                />
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                <AppTypography
                  variant="body2"
                  style={{
                    fontFamily: "monospace",
                    fontSize: "0.85rem",
                    ...responsiveTextStyles,
                  }}
                >
                  {image.shortId}
                </AppTypography>
              </AppTableCell>
              <AppTableCell align="right">
                <AppTypography variant="body2" style={responsiveTextStyles}>
                  {image.size} MB
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <AppTypography
                  variant="body2"
                  style={{
                    fontSize: "0.85rem",
                    ...responsiveTextStyles,
                  }}
                >
                  {image.created}
                </AppTypography>
              </AppTableCell>
              <AppTableCell align="center">
                <Chip
                  label={image.containers}
                  size="small"
                  variant="soft"
                  color={image.containers > 0 ? "success" : "default"}
                  style={{
                    minWidth: 40,
                  }}
                />
              </AppTableCell>
            </>
          )}
          renderExpandedContent={(image) => (
            <>
              <AppTypography variant="subtitle2" gutterBottom>
                <b>Full Image ID:</b>
              </AppTypography>
              <AppTypography
                variant="body2"
                style={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  marginBottom: 8,
                  ...longTextStyles,
                }}
              >
                {image.id}
              </AppTypography>

              <AppTypography variant="subtitle2" gutterBottom>
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
                      variant="soft"
                      sx={{
                        mr: 1,
                        mb: 1,
                        ...wrappableChipStyles,
                      }}
                    />
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no labels)
                  </AppTypography>
                )}
              </div>

              <AppTypography variant="subtitle2" gutterBottom>
                <b>Image Digests:</b>
              </AppTypography>
              <div>
                {image.raw.RepoDigests && image.raw.RepoDigests.length > 0 ? (
                  image.raw.RepoDigests.map((digest) => (
                    <AppTypography
                      key={digest}
                      variant="body2"
                      style={{
                        fontFamily: "monospace",
                        fontSize: "0.8rem",
                        marginBottom: 2,
                        ...longTextStyles,
                      }}
                    >
                      {digest}
                    </AppTypography>
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no digests)
                  </AppTypography>
                )}
              </div>
            </>
          )}
          emptyMessage="No images found."
        />
      )}

      <DeleteImageDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        imageIds={selectedImages.map((img) => img.id)}
        imageTags={selectedImages.map((img) => `${img.repo}:${img.tag}`)}
        onSuccess={handleDeleteSuccess}
      />
    </div>
  );
};
export default ImageList;
