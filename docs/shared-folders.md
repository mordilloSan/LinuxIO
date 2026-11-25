# Shared folders

**Storage → Shared folders** (`/storage/shares`) lists what the server
shares over SMB and NFS. There are two kinds:

- A **shared folder** is a folder LinuxIO made to be shared: a btrfs
  subvolume at the top of a pool, with a size limit and a snapshot schedule
  of its own and an **access table** that says which users and groups may
  use it. **Add Share → Create shared folder** makes one.
- A **plain share** is any other path, shared as its `smb.conf` section or
  its `/etc/exports` line says. **Add Share → Share an existing folder**
  makes one. It has no access table: LinuxIO does not change the
  permissions of a folder it did not make.

An existing folder cannot become a shared folder yet
([plan](./TODO/storage-redesign.md), Phase 5).

## A shared folder

A shared folder is recorded in `/etc/linuxio/shared-folders.json`
(`root:root 0600`): its pool (UUID), its path from the pool's top level, its
share name, its access table (each row with the UID or GID of the account it
was given to), whether it is shared over SMB and the NFS
clients it is exported to. The record is the source: the folder's
`smb.conf` section, its export and its ACL are written from it, and the
plain share routes refuse to change that section or export, or to put a
second one on the folder (409), since they would then no longer say what
the record says. A share name stays the folder's while it is not shared. A
new plain share of a folder inside a shared folder is refused too: a plain
share makes the folders missing on its path by path, as root, in a place the
folder's users may write. One that was there before can still be changed or
removed.

Only a pool with a permanent mount of its top level can hold one: the
section and the export name the folder by that mount point, and the mount
LinuxIO makes for its own work under `/run/linuxio/btrfs` goes away. That
top folder must be root's, with neither its group nor everyone allowed to
write it, or every operation that changes a shared folder is refused (409):
a user who may write it could rename a shared folder and put another folder
of the pool under its name, which root would then give to itself, open to
others or delete in its place. A pool LinuxIO creates has such a top folder.

### Create

`shares.create_folder` is one privileged route, so a failure cannot leave
half a folder behind. It first checks everything it was asked, touching
nothing: the name (one folder name that is also a share name, not
`linuxio-snapshots` and not a reserved section), the access table against
the local accounts, the snapshot rule, that no share has the name and no
folder is there, that nothing of an earlier folder of that name is left (an
SMB share or an export of its path, a snapshot schedule, a replication job,
snapshots), and
that the servers asked for are installed. What an earlier folder left would
otherwise be taken over: its snapshots listed, pruned and deleted as the new
folder's, its job sending it to the old target, and its share removed by an
undo. Then, in order:

1. the subvolume;
2. the size limit, which turns quotas on for the pool;
3. the snapshot rule;
4. the access, as the folder's ACL;
5. the record;
6. the SMB section and the NFS export.

A step that fails undoes the steps before it, newest first, and the last
one undoes itself, since it writes two files. The folder is new and empty,
so the undo deletes nothing of anyone's. Quotas stay on for the pool: they
are a setting of the pool, and other folders may have come to rely on them.
The undo runs even when the request was cancelled.

### Change, stop sharing

`shares.update_folder` replaces the share name, the access table and how
the folder is shared. A new name is a new **share name only**: the folder
keeps its path, and with it its snapshots, its replication jobs, its NFS
export and whatever mounts it. Clients connected under the old name connect
again under the new one.

The access is written to the folder first, then the record, then the
section and the export, so a change that takes access away holds on the
folder even if a later step fails. Each step goes by what the files hold,
and the section is found by its name or else by its path, so saving again
after a failure, a rename included, finishes the change.

A change of access reaches the folder itself and what is made in it
afterwards, not what is already inside, which keeps the entries it was made
with (see [Access](#access)). Over SMB the share's lists apply at once to
everything, and **no access** holds everywhere at once, since nobody it
names gets past the folder. Lowering someone to read only, or to everyone
else's lower level, therefore leaves them their old access to existing
files when they open them on the server or over NFS, and to files they made
themselves, which they own; the dialog says so when a change lowers access.

**Stop sharing** is the same route with SMB off and no NFS client: the
section and the export are removed, and the folder, what is in it, its
snapshots, its access table and its record stay, so it can be shared again.

### Delete folder and its data

`shares.delete_folder` deletes the folder with everything in it and its
snapshots. The request carries the folder's share name as typed by the
administrator, and the server refuses another (400). In order: the section
and the export, the subvolume, the snapshot rule, the replication jobs of
the folder, its snapshots, the record. The subvolume goes before its
snapshots and its schedule, so a folder that cannot be deleted (mounted, or
holding a subvolume someone made inside, which `btrfs subvolume delete`
refuses) keeps them. Copies a replication job already made on another pool
or server stay. The record goes last, so a delete that fails part-way is
still listed, off the network, and can be run again.

A folder whose pool is not mounted, or was destroyed, cannot be reached. The
same route then removes only what LinuxIO holds of it (its section, its
snapshot rule, its replication jobs and its record), so it does not stay
listed for ever, and the dialog says so (**Remove shared folder**). What is
on the pool stays, a folder that is no longer shared when the pool comes
back; an NFS export of it stays in `/etc/exports`, where it shows as a plain
share.

## Access

Each row of the table gives a local user or group **read and write**,
**read only** or **no access**, and one more level covers **everyone
else**. The rule:

- **No access wins** over everything.
- Otherwise a person gets the **most access** among their own row and the
  rows of their groups.
- **Everyone else** is whoever matches no row.

A row names a regular local account (UID or GID 1000 and up, not the
overflow account): the accounts of `/etc/passwd` and `/etc/group`. The
server keeps the account's UID or GID in the row, and a name that now
belongs to another ID is another account: the row no longer reaches it,
saving it is refused (400) until the row is removed and added again, and
applying the access drops it.

The rule is written twice, to the same effect.

**In the share's `smb.conf` section**, which Samba reads at each
connection:

| Line | Holds |
|------|-------|
| `invalid users` | the rows with no access; Samba checks it first |
| `write list` | the rows with read and write; a name in both lists may write |
| `read list` | the read-only rows, when everyone else may write |
| `valid users` | every row with access, when everyone else has none |
| `read only` | `no` when everyone else may write, else `yes` |
| `available = no` | when the table lets nobody in: an empty `valid users` would mean everyone |

A group is written `+name`, which looks it up in the system's groups only.
Sharing over SMB with a table that lets nobody in is refused (400).

**As the folder's POSIX ACL**, which the kernel reads for local and NFS
access and for everything Samba does as the person signed in. The folder is
root's; its owning group gets nothing; each row is a user or group entry;
everyone else is the "other" entry. The kernel lets a user's own entry
decide alone and otherwise grants what any of their groups grants, so the
rule is written into the user entries: a user with a row gets the strongest
level of that row and of their groups' rows, and each member of a group
with no access gets an entry of their own saying so, which no other group
can override.

The same entries are the folder's **default ACL**, which a file or folder
made inside starts with, so new contents follow the table. The folder has
the setgid bit, so what is made inside belongs to its group, root's, which
gets nothing: otherwise a file would belong to its maker's primary group,
and on a host where users share a primary group the kernel would stop at
that group's empty entry and keep its members from a file everyone else may
read. Samba makes a
file with the mode `create mask` allows, and the group bits of that mode cap
what the inherited entries give; with Samba's defaults the people with read
and write could not change what another of them made. The section therefore
sets `inherit acls = yes`, `create mask = 0666` and `directory mask = 0777`.

The ACL is written natively, as the two `system.posix_acl_*` attributes,
through the one handle `openBelow` gives ([Btrfs](./btrfs.md)), not with
`setfacl` on a path.

### When accounts change

A deleted user or group, a changed membership and a new user with groups
apply the access of every shared folder again (`shares.ReapplyAccess`):

- a row whose account is gone, or is another account under the same name,
  is dropped, from the folder's ACL and its section (also a section a failed
  stop sharing left behind), and then from the record; since a row is the
  account by its ID, an account made later under the same name never
  inherits it, even while the row is still there (what is already inside
  keeps entries for the old ID);
- the members of a group with no access are worked out anew.

The account change is done by then and is not undone when this fails; the
failure is logged, and the record keeps the row until a later change of an
account in LinuxIO applies it.

## Signing in from Windows and a Mac

Samba cannot check a password through PAM, since the client never sends
it, so it keeps a hash of its own for each user in its own list. A user
signs in to shared folders with their LinuxIO password once an
administrator switched that on for them, with **Can open shared folders
from Windows and Mac** in **Create user** or **Change password**. It is off
by default: the hash Samba keeps is a weak one, and many accounts here are
administrators.

A password set in LinuxIO with that on is written to Samba's list at the
moment it is set, the only moment LinuxIO has it (`smbpasswd -s -a`, the
password over stdin). The system password is set first. A user who existed
before sets their password once more to turn it on. A password changed in
LinuxIO for a user who has it on stays in step, also when the request does
not say; **Turn off without changing the password** takes the user out of
Samba's list (`shares.remove_samba_user`). Deleting a user removes them
from Samba's list first, while the account is there for Samba to look up.

Samba checks its own password and never the system one's lock, so **Lock**
in Accounts also disables the user's entry in Samba's list (`smbpasswd -d`),
before the system account, and **Unlock** enables it again
(`smbpasswd -e`), after. A Samba entry that cannot be changed does not keep
the account from being locked or unlocked; the error then says what is
left. A password set in LinuxIO enables it too, since
`chpasswd` unlocks the system account.

A password changed outside LinuxIO (`passwd` in a terminal) does not reach
Samba, which keeps the old one. `shares.list_samba_users` compares the day
`/etc/shadow` holds for the system password with the day Samba last set its
own, and Accounts flags the user (**Shared folders: old password**) when the
system one is newer.

## Routes

| Route | Privileged | Does |
|-------|------------|------|
| `shares.list_folders` | yes: the record is root's | the shared folders, with where each is on the host |
| `shares.create_folder`, `update_folder`, `delete_folder` | yes | as above |
| `shares.list_samba_users` | yes: Samba's list and `/etc/shadow` are root's | who can sign in, and whose password is stale |
| `shares.remove_samba_user` | yes | turns sign-in off for a user |
| `shares.list_samba_shares`, `list_nfs_shares` | no | the sections of `smb.conf` and the lines of `/etc/exports` |
| `shares.create_*`, `update_*`, `delete_*` for `samba_share` and `nfs_share` | yes | plain shares |

A session without administrator rights sees a shared folder as the plain
share it is published as.

Every operation on a shared folder, and every plain share route, runs under
one lock (`/run/linuxio-shared-folders.lock`), taken before the lock of
`smb.conf` and `/etc/exports` (`/run/linuxio-shares.lock`).

## Package removal

The record is LinuxIO's and goes with a purge of the DEB. The sections of
`smb.conf`, the exports, the folders with their ACLs and Samba's user list
are the administrator's setup, work without LinuxIO and stay.

## Known limits

- NFS trusts the user and group IDs its clients send, so over NFS the
  access table protects a folder only as well as those machines do.
- A membership changed outside LinuxIO (`usermod`, `gpasswd` in a terminal)
  reaches Samba at once and the folder's ACL at the next change of a folder
  or of an account in LinuxIO: until then a new member of a no-access group
  who is also in a group with access can use the folder locally and over
  NFS.
- A password changed outside LinuxIO is flagged to the day: one changed on
  the day LinuxIO set it goes unseen.
- Only local users and groups can have a row; accounts from a directory
  (LDAP, Active Directory) cannot.
- There is no guest access: everyone else means everyone with an account.
- A pool mounted at another place later leaves the folder's section and
  export naming the old one until the folder is saved again.
- A shared folder's subvolume deleted from the pool page leaves its share
  and its record behind; **Delete folder and its data** then removes them.
- A table change does not reach what is already inside the folder for
  local and NFS use (see [Change, stop sharing](#change-stop-sharing)).
- When Samba's user list cannot be read (`pdbedit` fails), deleting a user
  is refused, since the user's Samba password could otherwise pass to a
  later account of the same name, and a password change reports that the
  password for shared folders was not checked.
