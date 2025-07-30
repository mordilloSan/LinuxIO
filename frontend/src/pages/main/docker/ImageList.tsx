import { Box, Typography, Chip } from "@mui/material";
import { useQuery } from "@tanstack/react-query";

import { CollapsibleCardList } from "./DockerImageCard";

import { CollapsibleColumn } from "@/types/collapsible";
import axios from "@/utils/axios";

const formatImageRows = (images: any[]) =>
  images.flatMap((img) =>
    (img.RepoTags && img.RepoTags.length > 0
      ? img.RepoTags
      : ["<none>:<none>"]
    ).map((tag: string) => {
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
    }),
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
  const img = row.raw;
  return (
    <Box>
      <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
        Labels:
      </Typography>
      <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
        {img.Labels && Object.entries(img.Labels).length > 0 ? (
          Object.entries(img.Labels).map(([key, val]) => (
            <Chip
              key={key}
              label={`${key}: ${String(val)}`}
              size="small"
              sx={{
                mr: 1,
                mb: 1,
                fontSize: { xs: "0.75rem", sm: "0.875rem" },
                height: { xs: 22, sm: 26 },
              }}
            />
          ))
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
        {img.RepoDigests && img.RepoDigests.length > 0 ? (
          img.RepoDigests.map((digest: string) => (
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
          ))
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
  const { data } = useQuery({
    queryKey: ["dockerImages"],
    queryFn: async () => {
      const res = await axios.get("/docker/images");
      return res.data.output;
    },
  });

  const rows = data ? formatImageRows(data) : [];

  return (
    <CollapsibleCardList
      rows={rows}
      columns={imageColumns}
      renderCollapseContent={renderCollapseContent}
    />
  );
}
