import { useQuery } from "@tanstack/react-query";
import { Box, Typography, Chip } from "@mui/material";
import axios from "@/utils/axios";
import CollapsibleTable from "@/components/tables/CollapsibleTable";
import { CollapsibleColumn } from "@/types/collapsible";

const formatImageRows = (images: any[]) =>
    images.flatMap((img) =>
        (img.RepoTags && img.RepoTags.length > 0 ? img.RepoTags : ["<none>:<none>"]).map((tag: string) => {
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
        })
    );

// *** The fix is the type here: ***
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
            <Typography variant="subtitle2">Labels:</Typography>
            <Box sx={{ mb: 2 }}>
                {img.Labels && Object.entries(img.Labels).length > 0
                    ? Object.entries(img.Labels).map(([key, val]) => (
                        <Chip
                            key={key}
                            label={`${key}: ${String(val)}`}
                            size="small"
                            sx={{ mr: 1, mb: 1 }}
                        />
                    ))

                    : <Typography variant="body2" color="text.secondary">(no labels)</Typography>
                }
            </Box>
            <Typography variant="subtitle2">Image Digests:</Typography>
            <Box>
                {img.RepoDigests && img.RepoDigests.length > 0
                    ? img.RepoDigests.map((digest: string) => (
                        <Typography variant="body2" key={digest}>{digest}</Typography>
                    ))
                    : <Typography variant="body2" color="text.secondary">(no digests)</Typography>
                }
            </Box>
        </Box>
    );
}

export default function ImageList() {
    const { data, isLoading } = useQuery({
        queryKey: ["dockerImages"],
        queryFn: async () => {
            const res = await axios.get("/docker/images");
            return res.data.output;
        },
    });

    const rows = data ? formatImageRows(data) : [];

    return (
        <CollapsibleTable
            rows={rows}
            columns={imageColumns}
            renderCollapseContent={renderCollapseContent}
        />
    );
}
