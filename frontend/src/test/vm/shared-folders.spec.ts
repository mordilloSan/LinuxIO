import { randomUUID } from "node:crypto";

import { expect, test, type Page } from "@playwright/test";

import {
  bridgeCall,
  ensurePool,
  POOL_MOUNT,
  signIn,
  signOutIfSignedIn,
  vmDistro,
  vmSsh,
} from "./vm";

// docs/shared-folders.md: a shared folder is a subvolume with an access
// table, written as the lists of its smb.conf section and as the folder's
// ACL, and opened from Windows and a Mac with the LinuxIO password of a user
// that sign-in was switched on for. The checks here are the ones a unit test
// cannot make: a real smbd, a real password list and the kernel's ACLs.

// One user per level, and a second with read and write, who must be able to
// change what the first made.
const WRITER = "sfwriter";
const SECOND_WRITER = "sfwriter2";
const READER = "sfreader";
const REFUSED = "sfrefused";
const USERS = [WRITER, SECOND_WRITER, READER, REFUSED];
const PASSWORD = "Sf-e2e-Pass1";
const SHARE = "SfMedia";
const FOLDER = `${POOL_MOUNT}/${SHARE}`;

// A Fedora host with SELinux enforcing needs the folder labelled for Samba,
// which LinuxIO does not do yet (docs/TODO/backlog.md).
test.skip(vmDistro === "fedora", "Samba on SELinux is not set up by LinuxIO");

/** Runs a mutation of the signed-in bridge and expects it to succeed. */
async function mutate(page: Page, route: string, request: unknown) {
  const result = await bridgeCall(page, route, request, randomUUID());
  expect(result, `${route}: ${result.error ?? ""}`).toMatchObject({
    status: "ok",
  });
  return result.data;
}

/** What smbclient prints for commands run in the share as user. */
const smb = (user: string, commands: string) =>
  vmSsh(
    `smbclient //localhost/${SHARE} -U '${user}%${PASSWORD}' -c '${commands}' 2>&1 || true`,
  );

const removeLeftovers = () =>
  vmSsh(
    USERS.map(
      (user) =>
        `sudo smbpasswd -x ${user} >/dev/null 2>&1; sudo userdel -r ${user} >/dev/null 2>&1`,
    ).join("; ") + "; true",
  );

test.beforeAll(async () => {
  await vmSsh(
    "command -v smbd >/dev/null && command -v smbclient >/dev/null || sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq -o DPkg::Lock::Timeout=600 samba smbclient >/dev/null",
  );
  await removeLeftovers();
});

test.afterAll(removeLeftovers);
test.afterEach(({ page }) => signOutIfSignedIn(page));

test("a shared folder lets in over the network who its access table lets in", async ({
  page,
}) => {
  test.setTimeout(420_000);
  await signIn(page);
  await ensurePool(page);
  // A run that failed part-way left its folder behind.
  const before = (await (
    await bridgeCall(page, "shares.list_folders", {})
  ).data) as { uuid: string; path: string; name: string }[];
  for (const left of before.filter((folder) => folder.name === SHARE)) {
    await mutate(page, "shares.delete_folder", {
      uuid: left.uuid,
      path: left.path,
      confirmName: left.name,
    });
  }

  // Users with sign-in from Windows and a Mac switched on: the password
  // LinuxIO sets is also the one Samba holds.
  for (const username of USERS) {
    await mutate(page, "accounts.create_user", {
      username,
      password: PASSWORD,
      createHome: true,
      smbSignIn: true,
    });
  }
  const listed = (await (
    await bridgeCall(page, "shares.list_samba_users", {})
  ).data) as { username: string; passwordStale: boolean }[];
  expect(
    listed.filter((user) => USERS.includes(user.username)),
    "every user is in Samba's list, with the password of today",
  ).toEqual(
    [...USERS].sort().map((username) => ({ username, passwordStale: false })),
  );

  // The folder, made in one go from the Shared folders tab.
  await page.goto("/storage/shares");
  await page.getByRole("button", { name: "Add Share" }).click();
  await page.getByRole("menuitem", { name: "Create shared folder" }).click();
  const dialog = page.getByRole("dialog", { name: "Create shared folder" });
  await dialog.getByRole("combobox", { name: "Pool" }).click();
  await page.getByRole("option", { name: /e2e/ }).click();
  await dialog.getByLabel("Folder name").fill(SHARE);
  await dialog.getByRole("tab", { name: "Access" }).click();
  for (const [username, level] of [
    [WRITER, "Read and write"],
    [SECOND_WRITER, "Read and write"],
    [READER, "Read only"],
    [REFUSED, "No access"],
  ] as const) {
    await dialog.getByRole("combobox", { name: "Add a user or group" }).click();
    await page.getByRole("option", { name: username, exact: true }).click();
    await dialog
      .getByRole("radiogroup", { name: `Access for ${username}` })
      .getByRole("radio", { name: level })
      .check();
  }
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(dialog).toBeHidden({ timeout: 60_000 });

  // It is a subvolume with a snapshot rule, a record and a section.
  await vmSsh(`sudo btrfs subvolume show ${FOLDER} >/dev/null`);
  expect(
    await vmSsh("sudo cat /etc/linuxio/btrfs-snapshots.json"),
    "its snapshot rule",
  ).toContain(`"${SHARE}"`);
  expect(
    await vmSsh("sudo stat -c '%U:%G %a' /etc/linuxio/shared-folders.json"),
    "its record is root's alone",
  ).toBe("root:root 600\n");
  expect(await vmSsh("testparm -s 2>/dev/null"), "its section").toContain(
    `[${SHARE}]`,
  );

  // Over SMB. The refused user's password is good (the server lists its
  // shares for them); it is the folder that keeps them out.
  expect(await smb(WRITER, "put /etc/hostname one.txt; ls")).toContain(
    "one.txt",
  );
  const second = await smb(SECOND_WRITER, "put /etc/os-release one.txt; ls");
  expect(second, "a second writer changes what the first made").not.toMatch(
    /NT_STATUS/,
  );
  const reader = await smb(
    READER,
    "ls; get one.txt /tmp/sf-reader-copy; put /etc/hostname two.txt",
  );
  expect(reader, "the reader lists and reads").toContain("one.txt");
  expect(reader, "the reader cannot write").toMatch(
    /NT_STATUS_(ACCESS_DENIED|MEDIA_WRITE_PROTECTED)/,
  );
  expect(
    await smb(WRITER, "ls"),
    "nothing of the reader's is there",
  ).not.toContain("two.txt");
  expect(
    await vmSsh(
      `smbclient -L localhost -U '${REFUSED}%${PASSWORD}' 2>&1 || true`,
    ),
    "the refused user signs in to the server",
  ).toContain(SHARE);
  expect(await smb(REFUSED, "ls"), "and is kept out of the folder").toMatch(
    /NT_STATUS_ACCESS_DENIED/,
  );

  // On the host itself, the same table through the folder's ACL.
  expect(
    await vmSsh(
      `sudo -u ${WRITER} sh -c 'echo local > ${FOLDER}/local.txt && echo wrote'; ` +
        `sudo -u ${SECOND_WRITER} sh -c 'echo more >> ${FOLDER}/local.txt && echo appended'; ` +
        `sudo -u ${READER} sh -c 'cat ${FOLDER}/local.txt >/dev/null && echo read; touch ${FOLDER}/no.txt 2>/dev/null || echo refused'; ` +
        `sudo -u ${REFUSED} sh -c 'ls ${FOLDER} >/dev/null 2>&1 || echo refused'`,
    ),
  ).toBe("wrote\nappended\nread\nrefused\nrefused\n");

  // A locked account is kept out over SMB too, and let back in on unlock.
  await mutate(page, "accounts.lock_user", { username: READER });
  expect(await smb(READER, "ls"), "a locked user").toMatch(
    /NT_STATUS_(ACCOUNT_DISABLED|LOGON_FAILURE)/,
  );
  await mutate(page, "accounts.unlock_user", { username: READER });
  expect(await smb(READER, "ls"), "unlocked again").toContain("one.txt");

  // What a user makes inside belongs to the folder's group, root's.
  expect(await vmSsh(`stat -c %G ${FOLDER}/local.txt`)).toBe("root\n");

  // A system password newer than the one Samba holds: changed outside
  // LinuxIO. chage sets the day the password was last changed.
  await vmSsh(`sudo chage -d "$(date -d '+2 days' +%F)" ${READER}`);
  const afterChange = (await (
    await bridgeCall(page, "shares.list_samba_users", {})
  ).data) as { username: string; passwordStale: boolean }[];
  expect(afterChange.find((user) => user.username === READER)).toEqual({
    username: READER,
    passwordStale: true,
  });
  expect(afterChange.find((user) => user.username === WRITER)).toEqual({
    username: WRITER,
    passwordStale: false,
  });

  // A deleted user leaves Samba's list and the folder's access.
  await mutate(page, "accounts.delete_user", { username: REFUSED });
  expect(await vmSsh("sudo pdbedit -L")).not.toContain(REFUSED);
  expect(await vmSsh("sudo cat /etc/samba/smb.conf")).not.toContain(REFUSED);
  expect(
    await vmSsh("sudo cat /etc/linuxio/shared-folders.json"),
  ).not.toContain(REFUSED);

  // Delete folder and its data, after its name is typed.
  await page.goto("/storage/shares");
  const card = page
    .locator(".frosted-card, [class*='card']")
    .filter({ hasText: FOLDER })
    .last();
  await card.getByRole("button", { name: "Remove Share" }).click();
  await page
    .getByRole("menuitem", { name: "Delete folder and its data" })
    .click();
  const deleting = page.getByRole("dialog", {
    name: "Delete folder and its data",
  });
  const confirm = deleting.getByRole("button", {
    name: "Delete folder and its data",
  });
  await expect(confirm).toBeDisabled();
  await deleting.getByLabel(`Type ${SHARE} to confirm`).fill(SHARE);
  await confirm.click();
  await expect(deleting).toBeHidden({ timeout: 60_000 });
  expect(
    await vmSsh(`test -e ${FOLDER} && echo there || echo gone`),
    "the folder",
  ).toBe("gone\n");
  expect(await vmSsh("testparm -s 2>/dev/null")).not.toContain(`[${SHARE}]`);
  expect(
    await vmSsh("sudo cat /etc/linuxio/btrfs-snapshots.json"),
  ).not.toContain(`"${SHARE}"`);
  expect(
    await vmSsh("sudo cat /etc/linuxio/shared-folders.json"),
  ).not.toContain(SHARE);
});
