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
const SNAPSHOTS = `${POOL_MOUNT}/linuxio-snapshots/${SHARE}`;

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

/**
 * What smbclient prints for commands run in the share as user; options are
 * more of its own, such as the newest protocol it speaks.
 */
const smb = (user: string, commands: string, options = "") =>
  vmSsh(
    `smbclient //localhost/${SHARE} ${options} -U '${user}%${PASSWORD}' -c '${commands}' 2>&1 || true`,
  );

/** How many times this host is announced for an mDNS service type. */
const announced = async (type: string) =>
  Number(
    (
      await vmSsh(
        `avahi-browse -rpt ${type} 2>/dev/null | grep -c '^=' || true`,
      )
    ).trim(),
  );

/** The access and the default ACL of a folder, as the kernel holds them. */
const aclOf = (path: string) =>
  vmSsh(
    `sudo python3 -I -c 'import os, sys; print(*(os.getxattr(sys.argv[1], "system.posix_acl_" + kind).hex() for kind in ("access", "default")))' '${path}'`,
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
    "command -v smbd >/dev/null && command -v smbclient >/dev/null && command -v avahi-browse >/dev/null && systemctl cat wsdd-server.service >/dev/null 2>&1 || sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq -o DPkg::Lock::Timeout=600 samba smbclient avahi-daemon avahi-utils wsdd-server >/dev/null",
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
  const pool = await ensurePool(page);
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

  // Earlier versions. A new folder with snapshots offers them: the snapshot
  // taken now is listed for the file by its time, and opens as the file was.
  expect(await vmSsh("testparm -s 2>/dev/null")).toContain(
    `shadow:snapdir = ${SNAPSHOTS}`,
  );
  await mutate(page, "storage.btrfs_snapshot_create", {
    uuid: pool,
    path: SHARE,
  });
  await smb(WRITER, "put /etc/hostname one.txt");
  const versions = await smb(READER, "allinfo one.txt");
  const version = /@GMT-\d{4}\.\d\d\.\d\d-\d\d\.\d\d\.\d\d/.exec(versions)?.[0];
  expect(version, `an earlier version in: ${versions}`).toBeTruthy();
  await smb(READER, `get "${version}/one.txt" /tmp/sf-earlier.txt`);
  expect(
    await vmSsh(
      "cmp -s /tmp/sf-earlier.txt /etc/os-release && echo as-it-was; rm -f /tmp/sf-earlier.txt",
    ),
    "the earlier version holds what the file held when the snapshot was taken",
  ).toBe("as-it-was\n");
  // The snapshots are read by who may read the folder and nobody else, and
  // nobody but root lists the folder that holds every folder's snapshots.
  expect(
    await vmSsh(
      `sudo stat -c %a ${POOL_MOUNT}/linuxio-snapshots; ` +
        `sudo -u ${READER} sh -c 'ls ${SNAPSHOTS} >/dev/null && echo listed'; ` +
        `sudo -u ${REFUSED} sh -c 'ls ${SNAPSHOTS} >/dev/null 2>&1 || echo refused'; ` +
        `sudo -u ${READER} sh -c 'ls ${POOL_MOUNT}/linuxio-snapshots >/dev/null 2>&1 || echo no-listing'`,
    ),
  ).toBe("711\nlisted\nrefused\nno-listing\n");

  // The recycle bin. What is deleted over SMB goes to #recycle, which root
  // made with the folder's access; the move sets the time the worker goes
  // by, and its pass, run as the timer runs it, leaves a new file there.
  await smb(WRITER, "put /etc/hostname bin.txt");
  const moved = Number((await vmSsh("sleep 2; date +%s")).trim());
  expect(await smb(WRITER, "del bin.txt; ls #recycle/*")).toContain("bin.txt");
  expect(
    await vmSsh(`sudo stat -c %U '${FOLDER}/#recycle'`),
    "the bin is root's",
  ).toBe("root\n");
  expect(
    Number(
      (await vmSsh(`sudo stat -c %Z '${FOLDER}/#recycle/bin.txt'`)).trim(),
    ),
    "the move into the bin set the file's change time",
  ).toBeGreaterThanOrEqual(moved);
  expect(
    await smb(READER, "get #recycle/bin.txt /tmp/sf-bin.txt"),
    "the reader opens the bin",
  ).not.toMatch(/NT_STATUS/);
  // A folder in the bin is made by the first person to delete something
  // from it, and must take the next person's file too: Samba deletes for
  // good what it cannot move there.
  await smb(
    WRITER,
    "mkdir docs; put /etc/hostname docs/a.txt; put /etc/hostname docs/b.txt; del docs/a.txt",
  );
  const binned = await smb(SECOND_WRITER, "del docs/b.txt; ls #recycle/docs/*");
  expect(binned, "the first writer's deleted file").toContain("a.txt");
  expect(
    binned,
    "a second writer's deleted file lands in the folder the first made",
  ).toContain("b.txt");
  expect(
    await vmSsh(
      "systemctl is-enabled linuxio-shares-recycle.timer; systemctl is-active linuxio-shares-recycle.timer",
    ),
  ).toBe("enabled\nactive\n");
  expect(
    await vmSsh(
      "sudo systemctl start linuxio-shares-recycle.service; systemctl show -p Result --value linuxio-shares-recycle.service",
    ),
  ).toBe("success\n");
  expect(await vmSsh(`sudo ls '${FOLDER}/#recycle'`)).toContain("bin.txt");
  // The run noted, where only root reads it, when it first saw the folder
  // in the bin: what a folder holds is kept for 30 days from then.
  expect(
    await vmSsh(
      `sudo python3 -I -c 'import os, sys; print(os.getxattr(sys.argv[1], "trusted.linuxio.recycled").decode())' '${FOLDER}/#recycle/docs'`,
    ),
    "the folder's note",
  ).toMatch(/^\d+:\d+\n$/);
  expect(
    await vmSsh(`sudo ls '${FOLDER}/#recycle/docs'`),
    "and left what is in it",
  ).toContain("b.txt");

  // Time Machine: the folder is announced to Macs as a destination, by smbd
  // through Avahi, and earlier versions still open with the Mac modules on.
  const listedFolders = (await (
    await bridgeCall(page, "shares.list_folders", {})
  ).data) as ({ name: string; folder?: string } & Record<string, unknown>)[];
  const { folder: _place, ...mine } = listedFolders.find(
    (folder) => folder.name === SHARE,
  )!;
  await mutate(page, "shares.update_folder", {
    ...mine,
    timeMachine: true,
    timeMachineMaxBytes: 50 * 1024 ** 3,
  });
  const section = await vmSsh("testparm -s 2>/dev/null");
  expect(section).toContain("fruit:time machine = yes");
  expect(section).toContain("fruit:time machine max size = 51200M");
  await expect
    .poll(
      () =>
        vmSsh(
          "avahi-browse -rpt _adisk._tcp 2>/dev/null | grep '^=' | head -1 || true",
        ),
      {
        intervals: [2_000],
        message: "Time Machine announced",
        timeout: 40_000,
      },
    )
    .toContain(`adVN=${SHARE}`);
  expect(
    await smb(READER, `get "${version}/one.txt" /tmp/sf-earlier.txt`),
    "an earlier version with the Mac modules on",
  ).not.toMatch(/NT_STATUS/);

  // The SMB service's own settings. Samba's defaults, and the Windows
  // discovery service running as its package starts it.
  const settings = async () =>
    (await bridgeCall(page, "shares.get_smb_settings", {})).data;
  const defaults = {
    minProtocol: "SMB2",
    encryption: "default",
    macDiscovery: true,
    windowsDiscovery: true,
  };
  expect(await settings()).toEqual(defaults);
  // An older protocol is refused from the next connection on, by a reload.
  await mutate(page, "shares.set_smb_settings", {
    ...defaults,
    minProtocol: "SMB3",
    encryption: "desired",
  });
  const conf = await vmSsh("testparm -s 2>/dev/null");
  expect(conf).toContain("server min protocol = SMB3");
  expect(conf).toContain("server smb encrypt = desired");
  expect(await smb(READER, "ls", "-m SMB2"), "a client held to SMB2").toMatch(
    /NT_STATUS|failed/,
  );
  expect(await smb(READER, "ls"), "a client that speaks SMB3").toContain(
    "one.txt",
  );
  // Hidden from Macs and from Windows: smbd is restarted, which is when it
  // announces itself, and the discovery service stopped.
  expect(await announced("_smb._tcp")).toBeGreaterThan(0);
  await mutate(page, "shares.set_smb_settings", {
    ...defaults,
    macDiscovery: false,
    windowsDiscovery: false,
  });
  await expect
    .poll(() => announced("_smb._tcp"), {
      intervals: [2_000],
      message: "no longer announced to Macs",
      timeout: 40_000,
    })
    .toBe(0);
  expect(
    await vmSsh(
      "systemctl is-active wsdd-server.service; systemctl is-enabled wsdd-server.service; true",
    ),
  ).toBe("inactive\ndisabled\n");
  expect(await settings()).toEqual({
    ...defaults,
    macDiscovery: false,
    windowsDiscovery: false,
  });
  // And shown again.
  await mutate(page, "shares.set_smb_settings", defaults);
  await expect
    .poll(() => announced("_smb._tcp"), {
      intervals: [2_000],
      message: "announced to Macs again",
      timeout: 40_000,
    })
    .toBeGreaterThan(0);
  expect(await vmSsh("systemctl is-active wsdd-server.service")).toBe(
    "active\n",
  );

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

  // A deleted user leaves Samba's list and the folder's access, and the
  // recycle bin's with it: the bin has the folder's ACL before and after.
  const binBefore = await aclOf(`${FOLDER}/#recycle`);
  expect(binBefore, "the bin's ACL is the folder's").toBe(await aclOf(FOLDER));
  await mutate(page, "accounts.delete_user", { username: REFUSED });
  const binAfter = await aclOf(`${FOLDER}/#recycle`);
  expect(binAfter, "the bin's ACL followed the change").not.toBe(binBefore);
  expect(binAfter, "and is the folder's again").toBe(await aclOf(FOLDER));
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
  // With no bin left to empty, the timer is stopped.
  expect(
    await vmSsh("systemctl is-enabled linuxio-shares-recycle.timer || true"),
  ).toBe("disabled\n");
});
