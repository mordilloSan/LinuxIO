import { Icon } from "@iconify/react";
import React, { useCallback, useEffect, useState } from "react";

import CreateGroupDialog from "./components/CreateGroupDialog";
import DeleteGroupDialog from "./components/DeleteGroupDialog";
import EditGroupMembersDialog from "./components/EditGroupMembersDialog";

import { type AccountGroup, linuxio } from "@/api";
import GroupCard from "@/components/cards/GroupCard";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import Chip from "@/components/ui/AppChip";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSearchField from "@/components/ui/AppSearchField";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { responsiveTextStyles } from "@/theme/tableStyles";

interface GroupsTabProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

const GroupsTab: React.FC<GroupsTabProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const { data: groups = [] } = linuxio.accounts.list_groups.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editMembersDialogOpen, setEditMembersDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [selectedGroup, setSelectedGroup] = useState<AccountGroup | null>(null);
  const [groupToDelete, setGroupToDelete] = useState<AccountGroup | null>(null);

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

  const handleEditMembers = (group: AccountGroup) => {
    setSelectedGroup(group);
    setEditMembersDialogOpen(true);
  };

  const handleDelete = (group: AccountGroup) => {
    setGroupToDelete(group);
    setDeleteDialogOpen(true);
  };

  const columns: UnifiedTableColumn[] = [
    { field: "name", headerName: "Group Name", align: "left" },
    {
      field: "gid",
      headerName: "GID",
      align: "left",
      width: "80px",
      className: "app-table-hide-below-sm",
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
    <div>
      <div
        style={{
          marginBottom: 8,
          display: "flex",
          alignItems: "center",
          gap: 8,
          flexWrap: "wrap",
        }}
      >
        <AppSearchField
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search groups…"
          style={{ width: 320 }}
          value={search}
        />
        <span style={{ fontWeight: "bold" }}>{filtered.length} shown</span>
      </div>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <AppGrid container spacing={2}>
            {filtered.map((group) => (
              <AppGrid key={group.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <GroupCard
                  group={group}
                  onDelete={() => handleDelete(group)}
                  onEditMembers={() => handleEditMembers(group)}
                />
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div style={{ textAlign: "center", paddingBlock: 16 }}>
            <AppTypography color="text.secondary" variant="body2">
              No groups found.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          columns={columns}
          data={filtered}
          emptyMessage="No groups found."
          getRowKey={(group) => group.name}
          renderExpandedContent={(group) => (
            <div className="expand-panel">
              <AppTypography gutterBottom variant="subtitle2">
                <b>All Members ({group.members.length}):</b>
              </AppTypography>
              <div className="expand-panel__chips">
                {group.members.length > 0 ? (
                  group.members.map((member) => (
                    <Chip
                      key={member}
                      label={member}
                      size="small"
                      variant="soft"
                    />
                  ))
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    (no members)
                  </AppTypography>
                )}
              </div>
            </div>
          )}
          renderMainRow={(group) => (
            <>
              <AppTableCell>
                <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                  <AppTypography
                    fontWeight={500}
                    style={responsiveTextStyles}
                    variant="body2"
                  >
                    {group.name}
                  </AppTypography>
                  {group.isSystem && (
                    <Chip
                      label="system"
                      size="small"
                      style={{ fontSize: "0.65rem", height: 20 }}
                      variant="soft"
                    />
                  )}
                </div>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <AppTypography style={responsiveTextStyles} variant="body2">
                  {group.gid}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <div style={{ display: "flex", flexWrap: "wrap", gap: 2 }}>
                  {group.members.length > 0 ? (
                    group.members
                      .slice(0, 3)
                      .map((member) => (
                        <Chip
                          key={member}
                          label={member}
                          size="small"
                          style={{ fontSize: "0.7rem" }}
                          variant="soft"
                        />
                      ))
                  ) : (
                    <AppTypography color="text.secondary" variant="body2">
                      (no members)
                    </AppTypography>
                  )}
                  {group.members.length > 3 && (
                    <Chip
                      label={`+${group.members.length - 3}`}
                      size="small"
                      style={{ fontSize: "0.7rem" }}
                      variant="soft"
                    />
                  )}
                </div>
              </AppTableCell>
              <AppTableCell align="right">
                <div
                  style={{
                    display: "flex",
                    justifyContent: "flex-end",
                    gap: 2,
                  }}
                >
                  <AppTooltip title="Edit Members">
                    <AppIconButton
                      disabled={group.name === "root"}
                      onClick={(e) => {
                        e.stopPropagation();
                        handleEditMembers(group);
                      }}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:pencil" width={20} />
                    </AppIconButton>
                  </AppTooltip>
                  <AppTooltip title="Delete Group">
                    <AppIconButton
                      disabled={group.name === "root" || group.isSystem}
                      onClick={(e) => {
                        e.stopPropagation();
                        handleDelete(group);
                      }}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:delete" width={20} />
                    </AppIconButton>
                  </AppTooltip>
                </div>
              </AppTableCell>
            </>
          )}
        />
      )}

      <CreateGroupDialog
        onClose={() => setCreateDialogOpen(false)}
        open={createDialogOpen}
      />

      {selectedGroup && (
        <EditGroupMembersDialog
          group={selectedGroup}
          onClose={() => {
            setEditMembersDialogOpen(false);
            setSelectedGroup(null);
          }}
          open={editMembersDialogOpen}
        />
      )}

      {groupToDelete && (
        <DeleteGroupDialog
          groupNames={[groupToDelete.name]}
          onClose={() => {
            setDeleteDialogOpen(false);
            setGroupToDelete(null);
          }}
          onSuccess={() => {
            setDeleteDialogOpen(false);
            setGroupToDelete(null);
          }}
          open={deleteDialogOpen}
        />
      )}
    </div>
  );
};

export default GroupsTab;
