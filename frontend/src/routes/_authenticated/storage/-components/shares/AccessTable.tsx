import { Link } from "@tanstack/react-router";
import type { CSSProperties } from "react";

import type {
  AccountGroup,
  AccountUser,
  SambaUser,
  SharedFolderAccess,
} from "@/api";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppRadio from "@/components/ui/AppRadio";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";

import {
  ACCESS_LEVELS,
  accessCandidates,
  type AccessLevel,
  type AccessSubject,
  type AccessTable as AccessTableValue,
} from "./sharedFolders";

interface AccessTableProps {
  /** The signed-in administrator, marked "you" on their row. */
  currentUser?: string;
  groups: AccountGroup[];
  onChange: (value: AccessTableValue) => void;
  /**
   * Who can sign in from Windows and a Mac; leave out while the folder is
   * not shared over SMB or the list is not known, and nothing is said.
   */
  sambaUsers?: SambaUser[];
  users: AccountUser[];
  value: AccessTableValue;
}

// Who, the three levels, and the button that removes a row.
const rowStyle: CSSProperties = {
  alignItems: "center",
  display: "grid",
  gap: "var(--app-space-8)",
  gridTemplateColumns: "minmax(0, 1fr) repeat(3, 5.5rem) 2rem",
};

const levelCellStyle: CSSProperties = {
  display: "flex",
  justifyContent: "center",
  textAlign: "center",
};

const subjectKey = (subject: AccessSubject) =>
  `${subject.kind}:${subject.name}`;

/** Who a row is, under its name. */
const describeSubject = (
  row: SharedFolderAccess,
  users: AccountUser[],
  groups: AccountGroup[],
  currentUser: string | undefined,
): string => {
  if (row.kind === "group") {
    const members =
      groups.find((group) => group.name === row.name)?.members.length ?? 0;
    return members === 0
      ? "group"
      : `group · ${members} ${members === 1 ? "person" : "people"}`;
  }
  const fullName = users
    .find((user) => user.username === row.name)
    ?.gecos?.split(",")[0];
  return [row.name === currentUser && "you", fullName]
    .filter(Boolean)
    .join(" · ");
};

const LevelRadios = ({
  label,
  level,
  name,
  onChange,
}: {
  label: string;
  level: string;
  name: string;
  onChange: (level: AccessLevel) => void;
}) => (
  <div aria-label={label} role="radiogroup" style={{ display: "contents" }}>
    {ACCESS_LEVELS.map((option) => (
      <span key={option.level} style={levelCellStyle}>
        <AppRadio
          aria-label={option.label}
          checked={level === option.level}
          name={name}
          onChange={() => onChange(option.level)}
          size="small"
        />
      </span>
    ))}
  </div>
);

/**
 * The access table of a shared folder: a level for each user and group
 * given a row, and one for everyone else.
 */
const AccessTable = ({
  currentUser,
  groups,
  onChange,
  sambaUsers,
  users,
  value,
}: AccessTableProps) => {
  const candidates = accessCandidates(users, groups, value.access);
  const setLevel = (subject: AccessSubject, level: AccessLevel) =>
    onChange({
      ...value,
      access: value.access.map((row) =>
        subjectKey(row) === subjectKey(subject) ? { ...row, level } : row,
      ),
    });
  // A person with access who is not in Samba's list, or whose password
  // there is the old one, cannot open the folder from Windows or a Mac.
  const signInNotes = sambaUsers
    ? value.access
        .filter((row) => row.kind === "user" && row.level !== "none")
        .flatMap((row) => {
          const entry = sambaUsers.find((u) => u.username === row.name);
          if (!entry)
            return [`${row.name} cannot sign in from Windows or a Mac yet.`];
          return entry.passwordStale
            ? [
                `${row.name}'s password was changed outside LinuxIO, so the old one still opens shared folders.`,
              ]
            : [];
        })
    : [];

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-8)",
      }}
    >
      <div aria-hidden="true" style={rowStyle}>
        <AppTypography color="text.secondary" variant="caption">
          Who
        </AppTypography>
        {ACCESS_LEVELS.map((option) => (
          <AppTypography
            color="text.secondary"
            key={option.level}
            style={levelCellStyle}
            variant="caption"
          >
            {option.label}
          </AppTypography>
        ))}
        <span />
      </div>
      {value.access.map((row) => {
        const about = describeSubject(row, users, groups, currentUser);
        return (
          <div key={subjectKey(row)} style={rowStyle}>
            <div style={{ minWidth: 0 }}>
              <AppTypography fontWeight={700} noWrap variant="body2">
                {row.name}
              </AppTypography>
              {about && (
                <AppTypography color="text.secondary" noWrap variant="caption">
                  {about}
                </AppTypography>
              )}
            </div>
            <LevelRadios
              label={
                row.kind === "group"
                  ? `Access for the group ${row.name}`
                  : `Access for ${row.name}`
              }
              level={row.level}
              name={`access-${subjectKey(row)}`}
              onChange={(level) => setLevel(row, level)}
            />
            <AppActionIconButton
              icon="mdi:close"
              iconSize={18}
              label={`Remove ${row.name}`}
              onClick={() =>
                onChange({
                  ...value,
                  access: value.access.filter(
                    (other) => subjectKey(other) !== subjectKey(row),
                  ),
                })
              }
            />
          </div>
        );
      })}
      <div style={rowStyle}>
        <div style={{ minWidth: 0 }}>
          <AppTypography fontWeight={700} variant="body2">
            Everyone else
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            anyone with an account who has no row
          </AppTypography>
        </div>
        <LevelRadios
          label="Access for everyone else"
          level={value.everyone}
          name="access-everyone"
          onChange={(everyone) => onChange({ ...value, everyone })}
        />
        <span />
      </div>
      {candidates.length > 0 && (
        <AppSelect
          fullWidth
          label="Add a user or group"
          onChange={(event) => {
            const subject = candidates.find(
              (candidate) => subjectKey(candidate) === event.target.value,
            );
            if (subject)
              onChange({
                ...value,
                access: [...value.access, { ...subject, level: "ro" }],
              });
          }}
          value=""
        >
          <option value="">Choose who to add</option>
          {candidates.map((candidate) => (
            <option key={subjectKey(candidate)} value={subjectKey(candidate)}>
              {candidate.kind === "group"
                ? `${candidate.name} (group)`
                : candidate.name}
            </option>
          ))}
        </AppSelect>
      )}
      <AppTypography color="text.secondary" variant="caption">
        No access wins over everything. Otherwise a person gets the most access
        among their own row and their groups.
      </AppTypography>
      {signInNotes.length > 0 && (
        <AppAlert severity="info">
          {signInNotes.join(" ")} <Link to="/accounts">Set it in Accounts</Link>
        </AppAlert>
      )}
    </div>
  );
};

export default AccessTable;
