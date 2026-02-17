import DeleteIcon from "@mui/icons-material/Delete";
import EditIcon from "@mui/icons-material/Edit";
import {
  Box,
  TableCell,
  TextField,
  Chip,
  Typography,
  Checkbox,
  Button,
  IconButton,
  Tooltip,
} from "@mui/material";
import React, { useCallback, useEffect, useMemo, useState } from "react";

import CreateGroupDialog from "./components/CreateGroupDialog";
import DeleteGroupDialog from "./components/DeleteGroupDialog";
import EditGroupMembersDialog from "./components/EditGroupMembersDialog";

import { linuxio, type AccountGroup } from "@/api";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import { responsiveTextStyles } from "@/theme/tableStyles";

interface GroupsTabProps {
  onMountCreateHandler?: (handler: () => void) => void;
}

const GroupsTab: React.FC<GroupsTabProps> = ({ onMountCreateHandler }) => {
  const { data: groups = [] } = linuxio.accounts.list_groups.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [editMembersDialogOpen, setEditMembersDialogOpen] = useState(false);
  const [selectedGroup, setSelectedGroup] = useState<AccountGroup | null>(null);

  const groupsList = Array.isArray(groups) ? groups : [];

  const handleCreateGroup = useCallback(() => {
    setCreateDialogOpen(true);
  }, []);

  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleCreateGroup);
    }
  }, [onMountCreateHandler, handleCreateGroup]);

  const filtered = groupsList.filter(
    (group) =>
      group.name.toLowerCase().includes(search.toLowerCase()) ||
      group.members.some((m) => m.toLowerCase().includes(search.toLowerCase())),
  );

  const effectiveSelected = useMemo(() => {
    const filteredNames = new Set(filtered.map((g) => g.name));
    const result = new Set<string>();
    selected.forEach((name) => {
      if (filteredNames.has(name)) {
        result.add(name);
      }
    });
    return result;
  }, [selected, filtered]);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      // Don't allow selecting root
      setSelected(
        new Set(filtered.filter((g) => g.name !== "root").map((g) => g.name)),
      );
    } else {
      setSelected(new Set());
    }
  };

  const handleSelectOne = (name: string, checked: boolean) => {
    if (name === "root") return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(name);
      } else {
        next.delete(name);
      }
      return next;
    });
  };

  const handleDeleteSuccess = () => {
    setSelected(new Set());
  };

  const handleEditMembers = (group: AccountGroup) => {
    setSelectedGroup(group);
    setEditMembersDialogOpen(true);
  };

  const selectedGroups = filtered.filter((g) => effectiveSelected.has(g.name));
  const selectableGroups = filtered.filter((g) => g.name !== "root");
  const allSelected =
    selectableGroups.length > 0 &&
    effectiveSelected.size === selectableGroups.length;
  const someSelected =
    effectiveSelected.size > 0 &&
    effectiveSelected.size < selectableGroups.length;

  const columns: UnifiedTableColumn[] = [
    { field: "name", headerName: "Group Name", align: "left" },
    {
      field: "gid",
      headerName: "GID",
      align: "left",
      width: "80px",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
    {
      field: "members",
      headerName: "Members",
      align: "left",
    },
    {
      field: "actions",
      headerName: "Actions",
      align: "right",
      width: "100px",
    },
  ];

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2} flexWrap="wrap">
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search groups…"
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
        getRowKey={(group) => group.name}
        renderFirstCell={(group) => (
          <Checkbox
            size="small"
            checked={effectiveSelected.has(group.name)}
            onChange={(e) => handleSelectOne(group.name, e.target.checked)}
            onClick={(e) => e.stopPropagation()}
            disabled={group.name === "root"}
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
        renderMainRow={(group) => (
          <>
            <TableCell>
              <Box display="flex" alignItems="center" gap={1}>
                <Typography
                  variant="body2"
                  fontWeight="medium"
                  sx={responsiveTextStyles}
                >
                  {group.name}
                </Typography>
                {group.isSystem && (
                  <Chip
                    label="system"
                    size="small"
                    variant="outlined"
                    sx={{ fontSize: "0.65rem", height: 20 }}
                  />
                )}
              </Box>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
              <Typography variant="body2" sx={responsiveTextStyles}>
                {group.gid}
              </Typography>
            </TableCell>
            <TableCell>
              <Box display="flex" flexWrap="wrap" gap={0.5}>
                {group.members.length > 0 ? (
                  group.members
                    .slice(0, 3)
                    .map((member) => (
                      <Chip
                        key={member}
                        label={member}
                        size="small"
                        sx={{ fontSize: "0.7rem" }}
                      />
                    ))
                ) : (
                  <Typography variant="body2" color="text.secondary">
                    (no members)
                  </Typography>
                )}
                {group.members.length > 3 && (
                  <Chip
                    label={`+${group.members.length - 3}`}
                    size="small"
                    variant="outlined"
                    sx={{ fontSize: "0.7rem" }}
                  />
                )}
              </Box>
            </TableCell>
            <TableCell align="right">
              <Box display="flex" justifyContent="flex-end" gap={0.5}>
                <Tooltip title="Edit Members">
                  <IconButton
                    size="small"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleEditMembers(group);
                    }}
                    disabled={group.name === "root"}
                  >
                    <EditIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>
            </TableCell>
          </>
        )}
        renderExpandedContent={(group) => (
          <>
            <Typography variant="subtitle2" gutterBottom>
              <b>All Members ({group.members.length}):</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {group.members.length > 0 ? (
                group.members.map((member) => (
                  <Chip
                    key={member}
                    label={member}
                    size="small"
                    sx={{ mr: 1, mb: 1 }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no members)
                </Typography>
              )}
            </Box>
          </>
        )}
        emptyMessage="No groups found."
      />

      <CreateGroupDialog
        open={createDialogOpen}
        onClose={() => setCreateDialogOpen(false)}
      />

      <DeleteGroupDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        groupNames={selectedGroups.map((g) => g.name)}
        onSuccess={handleDeleteSuccess}
      />

      {selectedGroup && (
        <EditGroupMembersDialog
          open={editMembersDialogOpen}
          onClose={() => {
            setEditMembersDialogOpen(false);
            setSelectedGroup(null);
          }}
          group={selectedGroup}
        />
      )}
    </Box>
  );
};

export default GroupsTab;
