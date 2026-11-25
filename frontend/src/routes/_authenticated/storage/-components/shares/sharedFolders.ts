import type {
  AccountGroup,
  AccountUser,
  SharedFolder,
  SharedFolderAccess,
} from "@/api";

/** What a row of a shared folder's access table allows. */
export type AccessLevel = "rw" | "ro" | "none";

export const ACCESS_LEVELS: { level: AccessLevel; label: string }[] = [
  { level: "rw", label: "Read and write" },
  { level: "ro", label: "Read only" },
  { level: "none", label: "No access" },
];

export type AccessTable = Pick<SharedFolder, "access" | "everyone">;

/** Who a row names, without its level. */
export type AccessSubject = Pick<SharedFolderAccess, "kind" | "name">;

// Regular accounts, as the server tells them from system ones.
const FIRST_REGULAR_ID = 1000;
const NOBODY_ID = 65534;
const isRegularId = (id: number) => id >= FIRST_REGULAR_ID && id !== NOBODY_ID;

/** One line on who can use a folder, for its row in the list. */
export const accessSummary = ({ access, everyone }: AccessTable): string => {
  if (everyone === "rw") return "Everyone who signs in can read and write";
  if (everyone === "ro") return "Everyone who signs in can read";
  const granted = access.filter((row) => row.level !== "none");
  const users = granted.filter((row) => row.kind === "user").length;
  const groups = granted.length - users;
  if (granted.length === 0) return "Nobody has access";
  if (groups === 0)
    return users === 1 ? "1 person has access" : `${users} people have access`;
  if (users === 0)
    return groups === 1 ? "1 group has access" : `${groups} groups have access`;
  return `${granted.length} people and groups have access`;
};

/** The regular users and groups that have no row yet, users first. */
export const accessCandidates = (
  users: AccountUser[],
  groups: AccountGroup[],
  access: SharedFolderAccess[],
): AccessSubject[] => {
  const taken = new Set(access.map((row) => `${row.kind}:${row.name}`));
  return [
    ...users
      .filter((user) => isRegularId(user.uid))
      .map((user): AccessSubject => ({ kind: "user", name: user.username })),
    ...groups
      .filter((group) => isRegularId(group.gid))
      .map((group): AccessSubject => ({ kind: "group", name: group.name })),
  ].filter((subject) => !taken.has(`${subject.kind}:${subject.name}`));
};

/**
 * The table a new folder starts with: read and write for the administrator
 * making it and nobody else, or nobody at all when that account is one a
 * table cannot name (root) or the accounts are not known yet. Nobody is
 * never saved over SMB (accessProblem), so the administrator then chooses.
 */
export const defaultAccess = (
  username: string | undefined,
  users: AccountUser[],
): AccessTable => {
  const self = users.find((user) => user.username === username);
  return self && isRegularId(self.uid)
    ? {
        access: [{ kind: "user", name: self.username, level: "rw" }],
        everyone: "none",
      }
    : { access: [], everyone: "none" };
};

const RANK: Record<string, number> = { none: 0, ro: 1, rw: 2 };

/**
 * Whether a change of the table lowers someone to read only, or to everyone
 * else's lower level. The folder's ACL changes at once, and over SMB the
 * share's lists do, but a file or folder already inside keeps the entries it
 * was made with for local and NFS use. No access needs no warning: it holds
 * at the folder itself, which no one then gets past.
 */
export const accessLowered = (
  before: AccessTable,
  after: AccessTable,
): boolean => {
  const lowered = (from: string, to: string) =>
    to !== "none" && RANK[to] < RANK[from];
  return (
    lowered(before.everyone, after.everyone) ||
    before.access.some((row) => {
      const kept = after.access.find(
        (other) => other.kind === row.kind && other.name === row.name,
      );
      if (kept) return lowered(row.level, kept.level);
      // Without its row a person falls to their groups' rows or to everyone
      // else, which may still let them read: only read and write can drop
      // to read only that way.
      return row.level === "rw" && after.everyone !== "rw";
    })
  );
};

/** Why a table cannot be saved, or null. */
export const accessProblem = (
  access: SharedFolderAccess[],
  everyone: string,
  smb: boolean,
): string | null =>
  smb && everyone === "none" && access.every((row) => row.level === "none")
    ? "Give someone access before sharing over SMB"
    : null;

/**
 * The name a folder is announced to Macs under as a Time Machine
 * destination, "" when it is not one. The SMB service announces them when it
 * starts and never again, so the server restarts it when this changes.
 */
export const timeMachineName = (
  folder: Pick<SharedFolder, "name" | "smb" | "timeMachine">,
): string => (folder.smb && folder.timeMachine ? folder.name : "");

/** Said before a change that restarts the SMB service. */
export const SMB_RESTART_NOTE =
  "This restarts the SMB service, which is when Macs are told of Time Machine folders. Open connections are cut for a moment.";

/** Where a share is opened from Windows and from a Mac. */
export const networkAddresses = (host: string, share: string): string =>
  `\\\\${host}\\${share} on Windows, smb://${host}/${share} on a Mac`;
