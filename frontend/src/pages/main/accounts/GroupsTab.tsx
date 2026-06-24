import React, { useCallback, useState } from "react";

import CreateGroupDialog from "./components/CreateGroupDialog";
import DeleteGroupDialog from "./components/DeleteGroupDialog";
import EditGroupMembersDialog from "./components/EditGroupMembersDialog";

import { type AccountGroup, linuxio } from "@/api";
import GroupCard from "@/components/cards/GroupCard";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppGrid from "@/components/ui/AppGrid";
import AppSearchField from "@/components/ui/AppSearchField";
import AppTypography from "@/components/ui/AppTypography";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
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

  useRegisterCreateHandler(onMountCreateHandler, handleCreateGroup);

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

  const columns: AppDataTableColumnDef<AccountGroup>[] = [
    {
      accessorKey: "name",
      header: "Group Name",
      cell: ({ row }) => {
        const group = row.original;
        return (
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
        );
      },
      meta: { align: "left" },
    },
    {
      accessorKey: "gid",
      header: "GID",
      cell: ({ row }) => (
        <AppTypography style={responsiveTextStyles} variant="body2">
          {row.original.gid}
        </AppTypography>
      ),
      meta: {
        align: "left",
        hideBelow: "sm",
        width: "80px",
      },
    },
    {
      accessorFn: (group) => group.members.length,
      id: "members",
      header: "Members",
      cell: ({ row }) => {
        const group = row.original;
        return (
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
        );
      },
      meta: { align: "left" },
    },
    {
      id: "actions",
      header: "Actions",
      enableSorting: false,
      cell: ({ row }) => {
        const group = row.original;
        return (
          <div
            style={{
              display: "flex",
              justifyContent: "flex-end",
              gap: 2,
            }}
          >
            <AppActionIconButton
              disabled={group.name === "root"}
              icon="mdi:pencil"
              iconSize={20}
              label="Edit Members"
              onClick={(e) => {
                e.stopPropagation();
                handleEditMembers(group);
              }}
            />
            <AppActionIconButton
              disabled={group.name === "root" || group.isSystem}
              icon="mdi:delete"
              iconSize={20}
              label="Delete Group"
              onClick={(e) => {
                e.stopPropagation();
                handleDelete(group);
              }}
            />
          </div>
        );
      },
      meta: {
        align: "right",
        width: "100px",
      },
    },
  ];

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      <div
        style={{
          display: "flex",
          flexShrink: 0,
          flexWrap: "wrap",
          alignItems: "center",
          gap: 8,
          marginBottom: 8,
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
        <AppDataTable
          ariaLabel="Groups"
          columns={columns}
          data={filtered}
          emptyMessage="No groups found."
          fillAvailable
          getRowId={(group) => group.name}
          renderExpandedContent={({ original: group }) => (
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
