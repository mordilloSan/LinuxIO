import { useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";

import { type AccountGroup, linuxio } from "@/api";
import GroupCard from "@/components/cards/GroupCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import { responsiveTextStyles } from "@/theme/tableStyles";

import CreateGroupDialog from "./components/CreateGroupDialog";
import DeleteGroupDialog from "./components/DeleteGroupDialog";
import EditGroupMembersDialog from "./components/EditGroupMembersDialog";

interface GroupsTabProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

const getAccountGroupId = (group: AccountGroup) => group.name;

// The inline panel reads only the row's group, so one module-level renderer
// keeps the table's props stable across re-renders.
const renderGroupExpandedContent = ({
  original: group,
}: {
  original: AccountGroup;
}) => (
  <div className="expand-panel">
    <AppTypography gutterBottom variant="subtitle2">
      <b>All Members ({group.members.length}):</b>
    </AppTypography>
    <div className="expand-panel__chips">
      {group.members.length > 0 ? (
        group.members.map((member) => (
          <Chip key={member} label={member} size="small" variant="soft" />
        ))
      ) : (
        <AppTypography color="text.secondary" variant="body2">
          (no members)
        </AppTypography>
      )}
    </div>
  </div>
);

const GroupsTab = ({
  onMountCreateHandler,
  viewMode = "table",
}: GroupsTabProps) => {
  const { data: groups } = useSuspenseQuery({
    ...linuxio.accounts.list_groups,
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

  const surface = useReorderableSurface({
    getId: getAccountGroupId,
    items: groupsList,
    surface: "accounts.groups",
  });
  const tableDnd = useReorderableTableDnd<AccountGroup, AccountGroup>({
    handleAriaLabel: "Reorder group",
    surface,
  });

  const filtered = useMemo(
    () =>
      surface.items.filter(
        (group) =>
          group.name.toLowerCase().includes(search.toLowerCase()) ||
          group.members.some((m) =>
            m.toLowerCase().includes(search.toLowerCase()),
          ),
      ),
    [search, surface.items],
  );

  const handleEditMembers = useCallback((group: AccountGroup) => {
    setSelectedGroup(group);
    setEditMembersDialogOpen(true);
  }, []);

  const handleDelete = useCallback((group: AccountGroup) => {
    setGroupToDelete(group);
    setDeleteDialogOpen(true);
  }, []);

  // Stable column defs: cells render through flexRender, so a rebuilt array
  // remounts every cell subtree — including on the press that arms a
  // reorder hold. See docs/table-row-gestures.md.
  const columns = useMemo<AppVirtualTableColumnDef<AccountGroup>[]>(
    () => [
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
                <Chip label="system" size="xsmall" variant="soft" />
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
                      size="xsmall"
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
                  size="xsmall"
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
    ],
    [handleDelete, handleEditMembers],
  );

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      <RoutedTabSearch active={search !== ""}>
        <AppHeaderSearch
          clearOnDocumentEscape
          onChange={setSearch}
          placeholder="Search groups…"
          value={search}
        />
      </RoutedTabSearch>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <ReorderableCardGrid
            fillAvailable
            getId={getAccountGroupId}
            items={filtered}
            renderItem={(group) => (
              <GroupCard
                group={group}
                onDelete={() => handleDelete(group)}
                onEditMembers={() => handleEditMembers(group)}
              />
            )}
            size={CARD_GRID_SIZE_STANDARD}
            surface={surface}
          />
        ) : (
          <div style={{ textAlign: "center", paddingBlock: 16 }}>
            <AppTypography color="text.secondary" variant="body2">
              No groups found.
            </AppTypography>
          </div>
        )
      ) : (
        <AppVirtualTable
          ariaLabel="Groups"
          columns={columns}
          data={filtered}
          dnd={tableDnd}
          emptyMessage="No groups found."
          fillAvailable
          getRowId={getAccountGroupId}
          persistExpandedKey="account-groups"
          renderExpandedContent={renderGroupExpandedContent}
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
