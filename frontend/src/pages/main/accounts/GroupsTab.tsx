import { Icon } from "@iconify/react";
import React, { useCallback, useEffect, useMemo, useState } from "react";

import CreateGroupDialog from "./components/CreateGroupDialog";
import DeleteGroupDialog from "./components/DeleteGroupDialog";
import EditGroupMembersDialog from "./components/EditGroupMembersDialog";

import { linuxio, type AccountGroup } from "@/api";
import GroupCard from "@/components/cards/GroupCard";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
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
          placeholder="Search groups…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{ width: 320 }}
        />
        <span style={{ fontWeight: "bold" }}>{filtered.length} shown</span>
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
            {filtered.map((group) => (
              <AppGrid key={group.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <GroupCard
                  group={group}
                  selected={effectiveSelected.has(group.name)}
                  onSelect={(checked) => handleSelectOne(group.name, checked)}
                  onEditMembers={() => handleEditMembers(group)}
                />
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div style={{ textAlign: "center", paddingBlock: 16 }}>
            <AppTypography variant="body2" color="text.secondary">
              No groups found.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          data={filtered}
          columns={columns}
          getRowKey={(group) => group.name}
          renderFirstCell={(group) => (
            <AppCheckbox
              size="small"
              checked={effectiveSelected.has(group.name)}
              onChange={(e) => handleSelectOne(group.name, e.target.checked)}
              onClick={(e) => e.stopPropagation()}
              disabled={group.name === "root"}
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
          renderMainRow={(group) => (
            <>
              <AppTableCell>
                <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                  <AppTypography
                    variant="body2"
                    fontWeight={500}
                    style={responsiveTextStyles}
                  >
                    {group.name}
                  </AppTypography>
                  {group.isSystem && (
                    <Chip
                      label="system"
                      size="small"
                      variant="soft"
                      style={{ fontSize: "0.65rem", height: 20 }}
                    />
                  )}
                </div>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <AppTypography variant="body2" style={responsiveTextStyles}>
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
                          variant="soft"
                          style={{ fontSize: "0.7rem" }}
                        />
                      ))
                  ) : (
                    <AppTypography variant="body2" color="text.secondary">
                      (no members)
                    </AppTypography>
                  )}
                  {group.members.length > 3 && (
                    <Chip
                      label={`+${group.members.length - 3}`}
                      size="small"
                      variant="soft"
                      style={{ fontSize: "0.7rem" }}
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
                      size="small"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleEditMembers(group);
                      }}
                      disabled={group.name === "root"}
                    >
                      <Icon icon="mdi:pencil" width={20} height={20} />
                    </AppIconButton>
                  </AppTooltip>
                </div>
              </AppTableCell>
            </>
          )}
          renderExpandedContent={(group) => (
            <>
              <AppTypography variant="subtitle2" gutterBottom>
                <b>All Members ({group.members.length}):</b>
              </AppTypography>
              <div
                style={{ marginBottom: 8, display: "flex", flexWrap: "wrap" }}
              >
                {group.members.length > 0 ? (
                  group.members.map((member) => (
                    <Chip
                      key={member}
                      label={member}
                      size="small"
                      variant="soft"
                      style={{ marginRight: 4, marginBottom: 4 }}
                    />
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no members)
                  </AppTypography>
                )}
              </div>
            </>
          )}
          emptyMessage="No groups found."
        />
      )}

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
    </div>
  );
};

export default GroupsTab;
