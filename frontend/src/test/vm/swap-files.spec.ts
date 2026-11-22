import { expect, type Page, test } from "@playwright/test";

import { signIn, signOutIfSignedIn, stopAptRuns, vmDistro, vmSsh } from "./vm";

// The swap file LinuxIO creates, resizes and removes from the dashboard's
// Memory overflow dialog (docs/memory.md, "The dialog"), driven through the
// dialog alone against the bridge of the installed package, with the VM's own
// state read as root after each step: /proc/swaps, /etc/fstab and the file.
// Last, with zram-tools installed and its service kept from starting, the
// file is made again and a switch to compressed memory removes it.
//
// It is written for Ubuntu, whose clean image has an ext4 root and no swap at
// all. It leaves no swap, no zram tool, and the sysctls, zswap and the files
// a mode change writes as it found them: memory.spec.ts needs a VM without
// swap.

const OWN = "/var/lib/linuxio/swap/swapfile";
const TEMPORARY = `${OWN}.linuxio-resize`;
const FSTAB_LINE = `${OWN} none swap sw,nofail 0 0`;
const MiB = 1024 * 1024;
// A swap file's area in /proc/swaps is its length less the header page, in KiB.
const swapKiB = (mib: number) => mib * 1024 - 4;

// What a mode change writes and sets (docs/memory.md, "Files written on the
// host"), saved before the tests and put back after them.
const SYSCTLS = [
  "vm.swappiness",
  "vm.page-cluster",
  "vm.watermark_boost_factor",
  "vm.watermark_scale_factor",
];
const ZSWAP_ENABLED = "/sys/module/zswap/parameters/enabled";
const MODE_FILES = [
  "/etc/sysctl.d/90-linuxio-memory.conf",
  "/etc/tmpfiles.d/linuxio-zswap.conf",
];

// Takes away what the tests make, also after a cut run: LinuxIO's swap file
// and its temporary file off, out of /etc/fstab and deleted, its empty folder
// removed, and zram-tools purged, whose prerm stops zramswap.service.
const CLEAN_UP = String.raw`set -euo pipefail
own=${OWN}
for file in "$own" "$own.linuxio-resize"; do
	if awk -v f="$file" '$1 == f {found = 1} END {exit !found}' /proc/swaps; then swapoff "$file"; fi
done
if awk -v f="$own" '$1 == f {found = 1} END {exit !found}' /etc/fstab; then
	awk -v f="$own" '$1 != f' /etc/fstab >/etc/fstab.e2e
	cat /etc/fstab.e2e >/etc/fstab
	rm -f /etc/fstab.e2e
	systemctl daemon-reload
fi
rm -f "$own" "$own.linuxio-resize"
rmdir --ignore-fail-on-non-empty "$(dirname "$own")" 2>/dev/null || true
if dpkg -s zram-tools >/dev/null 2>&1; then
	DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 purge -y -qq zram-tools >/dev/null
fi
if grep -q '^/dev/zram' /proc/swaps; then
	echo "zram still swaps with zram-tools purged:" >&2
	cat /proc/swaps >&2
	exit 1
fi
`;

// zram-tools installed the way the memory root suite has it: present, with
// its service disabled and never started. A policy-rc.d that refuses every
// start keeps the package's postinst from starting zramswap.service.
const INSTALL_ZRAM_TOOLS = String.raw`set -euo pipefail
if ! dpkg -s zram-tools >/dev/null 2>&1; then
	if [[ -e /usr/sbin/policy-rc.d ]]; then
		echo "/usr/sbin/policy-rc.d is there already" >&2
		exit 1
	fi
	printf '#!/bin/sh\nexit 101\n' >/usr/sbin/policy-rc.d
	chmod 0755 /usr/sbin/policy-rc.d
	trap 'rm -f /usr/sbin/policy-rc.d' EXIT
	DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y -qq zram-tools >/dev/null
fi
systemctl disable --now zramswap.service 2>/dev/null
`;

/** The VM's swap areas by path, each with its size in KiB as /proc/swaps gives it. */
async function procSwaps(): Promise<Record<string, number>> {
  const out = await vmSsh("tail -n +2 /proc/swaps");
  return Object.fromEntries(
    out
      .split("\n")
      .filter(Boolean)
      .map((line) => {
        const fields = line.trim().split(/\s+/);
        return [fields[0], Number(fields[2])];
      }),
  );
}

/** The swap lines of the VM's /etc/fstab, as written. */
const fstabSwapLines = async () =>
  (await vmSsh(`awk '$3 == "swap"' /etc/fstab`)).split("\n").filter(Boolean);

const fstabSum = async () =>
  (await vmSsh("sha256sum /etc/fstab")).split(" ")[0];

/** Mode, owner, group, length and type of path in the VM; "missing" when there is none. */
const fileState = async (path: string) =>
  (
    await vmSsh(
      `sudo stat -c '%a %U %G %s %F' ${path} 2>/dev/null || echo missing`,
    )
  ).trim();

/** The root job host's lines about swap since the VM's clock read since, for the report. */
const swapJournal = async (since: string) =>
  (
    await vmSsh(
      `sudo journalctl --no-pager -b -u linuxio-jobs@0.service -o json --since @${since} | ` +
        `jq -c 'select(.MESSAGE | test("swap")) | {message: .MESSAGE} + (with_entries(select(.key | startswith("LINUXIO_"))))'`,
    )
  ).trim();

const vmClock = async () => (await vmSsh("date +%s")).trim();

const overflowDialog = (page: Page) =>
  page.getByRole("dialog", { name: "Memory overflow" });

const swapFiles = (page: Page) =>
  overflowDialog(page).getByRole("list", { name: "Swap files" });

const ownRow = (page: Page) =>
  swapFiles(page).getByRole("listitem").filter({ hasText: OWN });

const toastOf = (page: Page, text: string | RegExp) =>
  page.locator("[data-sonner-toast]").filter({ hasText: text });

/** Signs in and opens the dialog from the Swap row of the dashboard's Memory card. */
async function openOverflowDialog(page: Page) {
  await signIn(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Edit Swap", exact: true }).click();
  await expect(overflowDialog(page)).toBeVisible();
  // The rows show once the status is read.
  await expect(swapFiles(page)).toBeVisible({ timeout: 30_000 });
}

/**
 * Keeps every name the task tray's peek gives its running job from now on.
 * A swap file job can end within a second, and the tray lists a job only
 * while it runs, so the names are caught as the page renders them rather
 * than looked for afterwards. The peek is always in the footer and names the
 * running job the tray lists, whether or not it is showing.
 */
const recordTray = (page: Page) =>
  page.evaluate(() => {
    const seen: string[] = [];
    (window as unknown as { __tray: string[] }).__tray = seen;
    const keep = (label: string | null | undefined) => {
      if (label && label !== seen.at(-1)) seen.push(label);
    };
    new MutationObserver((records) => {
      for (const record of records) {
        const target = record.target as Element;
        if (
          record.type === "attributes" &&
          target.classList.contains("app-navbar-notifications__peek")
        ) {
          keep(record.oldValue);
          keep(target.getAttribute("aria-label"));
        }
      }
    }).observe(document.body, {
      attributeFilter: ["aria-label"],
      attributeOldValue: true,
      subtree: true,
    });
  });

const trayNames = (page: Page) =>
  page.evaluate(() => (window as unknown as { __tray: string[] }).__tray);

/**
 * The routes of the calls the page opens on its bridge socket, in order. The
 * listener goes on before the page opens its socket.
 */
function recordCalls(page: Page): string[] {
  const routes: string[] = [];
  page.on("websocket", (socket) => {
    socket.on("framesent", ({ payload }) => {
      const text =
        typeof payload === "string" ? payload : payload.toString("latin1");
      for (const match of text.matchAll(/"route":"([a-z_.]+)"/g)) {
        routes.push(match[1]);
      }
    });
  });
  return routes;
}

// Saved before the tests, put back after them.
let saved: { sysctls: string; zswap: string; files: string } | undefined;

test.describe
  .serial("LinuxIO's swap file from the Memory overflow dialog", () => {
  test.skip(
    vmDistro !== "ubuntu",
    "Ubuntu only: its clean image has an ext4 root and no swap",
  );

  test.beforeAll(async () => {
    test.setTimeout(240_000);
    await vmSsh("sudo bash -s", CLEAN_UP);
    saved = {
      sysctls: (await vmSsh(`sysctl -n ${SYSCTLS.join(" ")}`)).trim(),
      zswap: (await vmSsh(`cat ${ZSWAP_ENABLED} 2>/dev/null || true`)).trim(),
      files: (
        await vmSsh(`ls ${MODE_FILES.join(" ")} 2>/dev/null || true`)
      ).trim(),
    };
  });

  test.afterEach(async ({ page }) => signOutIfSignedIn(page));

  test.afterAll(async () => {
    test.setTimeout(240_000);
    await vmSsh("sudo bash -s", CLEAN_UP);
    if (!saved) return;
    const values = saved.sysctls.split("\n");
    await vmSsh(
      `sudo sysctl -q ${SYSCTLS.map((key, i) => `${key}=${values[i]}`).join(" ")}`,
    );
    if (saved.zswap) {
      await vmSsh(`echo ${saved.zswap} | sudo tee ${ZSWAP_ENABLED} >/dev/null`);
    }
    const kept = saved.files.split("\n");
    const added = MODE_FILES.filter((file) => !kept.includes(file));
    if (added.length > 0) await vmSsh(`sudo rm -f ${added.join(" ")}`);
  });

  test("1. with no swap the dialog says so and offers Create", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    expect(await procSwaps(), "/proc/swaps").toEqual({});
    expect(await fstabSwapLines(), "swap lines in /etc/fstab").toEqual([]);

    await openOverflowDialog(page);
    await expect(
      swapFiles(page).getByText(
        "No swap is on, and no swap file is in /etc/fstab.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(
      overflowDialog(page).getByText(
        "If memory runs out, Linux will close programs.",
      ),
    ).toBeVisible();
    await expect(
      overflowDialog(page).getByRole("button", { name: "Create a swap file" }),
    ).toBeEnabled();
  });

  test("2. Create makes the file at 256 MiB, in /etc/fstab and swapping", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await openOverflowDialog(page);
    await overflowDialog(page)
      .getByRole("button", { name: "Create a swap file" })
      .click();
    const step = page.getByRole("dialog", { name: "Create a swap file" });
    await expect(step).toBeVisible();
    await expect(
      step.getByText(
        `LinuxIO creates the swap file ${OWN}, adds it to /etc/fstab and turns it on.`,
      ),
    ).toBeVisible();
    await step.getByRole("spinbutton", { name: "Size in MiB" }).fill("256");

    const since = await vmClock();
    await recordTray(page);
    await step.getByRole("button", { name: "Create", exact: true }).click();
    // The step closes as the job starts; the dialog's first step is back.
    await expect(step).toBeHidden();
    await expect(overflowDialog(page)).toBeVisible();

    await expect(toastOf(page, "Swap file created")).toBeVisible({
      timeout: 60_000,
    });
    const row = ownRow(page);
    await expect(row).toBeVisible({ timeout: 30_000 });
    await expect(row).toContainText("256 MB");
    await expect(
      overflowDialog(page).getByRole("button", { name: "Create a swap file" }),
    ).toHaveCount(0);
    await expect(
      row.getByRole("button", { name: `Resize ${OWN}` }),
    ).toBeEnabled();
    await expect(
      row.getByRole("button", { name: `Remove ${OWN}` }),
    ).toBeEnabled();

    const tray = await trayNames(page);
    console.log(`tray while creating: ${tray.join(" | ")}`);
    console.log(`job host: ${await swapJournal(since)}`);
    expect
      .soft(tray, "the tray named the running job")
      .toContainEqual(
        expect.stringMatching(
          /^Open notifications: Creating (a swap file|the swap file \/var\/lib\/linuxio\/swap\/swapfile)/,
        ),
      );

    expect(await fileState(OWN)).toBe(
      `600 root root ${256 * MiB} regular file`,
    );
    expect(await fileState("/var/lib/linuxio/swap")).toMatch(
      /^755 root root \d+ directory$/,
    );
    expect(await procSwaps()).toEqual({ [OWN]: swapKiB(256) });
    expect(await fstabSwapLines()).toEqual([FSTAB_LINE]);
  });

  test("3. Resize turns it into 320 MiB in place, /etc/fstab unchanged", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    const fstabBefore = await fstabSum();
    await openOverflowDialog(page);
    await ownRow(page)
      .getByRole("button", { name: `Resize ${OWN}` })
      .click();
    const step = page.getByRole("dialog", { name: `Resize ${OWN}` });
    await expect(step).toBeVisible();
    await expect(step.getByText(`${OWN} is 256 MB.`)).toBeVisible();
    await expect(
      step.getByText(
        "To change its size, LinuxIO turns the file off and on again. Data parked in it is brought back to memory first.",
      ),
    ).toBeVisible();
    const field = step.getByRole("spinbutton", { name: "Size in MiB" });
    await expect(field).toHaveValue("256");
    // The size it has already is not a change.
    const resize = step.getByRole("button", { name: "Resize", exact: true });
    await expect(resize).toBeDisabled();
    await field.fill("320");

    const since = await vmClock();
    await recordTray(page);
    await resize.click();
    await expect(step).toBeHidden();
    await expect(overflowDialog(page)).toBeVisible();

    await expect(toastOf(page, `Resized ${OWN}`)).toBeVisible({
      timeout: 60_000,
    });
    await expect(ownRow(page)).toContainText("320 MB", { timeout: 30_000 });

    const tray = await trayNames(page);
    console.log(`tray while resizing: ${tray.join(" | ")}`);
    console.log(`job host: ${await swapJournal(since)}`);
    expect
      .soft(tray, "the tray named the running job")
      .toContainEqual(
        expect.stringMatching(
          /^Open notifications: (Resizing|Adding the temporary swap file|Removing the temporary swap file) /,
        ),
      );

    expect(await fileState(OWN)).toBe(
      `600 root root ${320 * MiB} regular file`,
    );
    expect(await fileState(TEMPORARY), "the temporary swap file").toBe(
      "missing",
    );
    expect(await procSwaps()).toEqual({ [OWN]: swapKiB(320) });
    expect(await fstabSum(), "/etc/fstab").toBe(fstabBefore);
    expect(await fstabSwapLines()).toEqual([FSTAB_LINE]);
  });

  test("4. the size field refuses a size below the minimum or above the maximum without a request", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    const calls = recordCalls(page);
    await openOverflowDialog(page);
    await ownRow(page)
      .getByRole("button", { name: `Resize ${OWN}` })
      .click();
    const step = page.getByRole("dialog", { name: `Resize ${OWN}` });
    const field = step.getByRole("spinbutton", { name: "Size in MiB" });
    const resize = step.getByRole("button", { name: "Resize", exact: true });
    await expect(field).toHaveValue("320");

    const help = await step
      .getByText(/^From \d+ to \d+ MiB\. \d+ MiB is the largest size/)
      .innerText();
    const [, min, max] = help.match(/^From (\d+) to (\d+) MiB/)!.map(Number);
    console.log(
      `size field: ${help}; the VM's root: ${(await vmSsh("df -B1M --output=size,avail / | tail -1")).trim()} (MiB size, available)`,
    );
    expect(min, "the smallest size, in MiB").toBe(256);
    expect(max, "the largest size, in MiB").toBeGreaterThan(320);

    await field.fill(String(min - 1));
    await expect(step.getByText(`At least ${min} MiB.`)).toBeVisible();
    await expect(resize).toBeDisabled();
    await field.press("Enter");

    await field.fill(String(max + 1));
    await expect(
      step.getByText(`At most ${max} MiB, the largest size the disk allows.`),
    ).toBeVisible();
    await expect(resize).toBeDisabled();
    await field.press("Enter");

    await field.fill(String(max));
    await expect(resize).toBeEnabled();
    await step.getByRole("button", { name: "Back", exact: true }).click();
    await expect(overflowDialog(page)).toBeVisible();

    // The recorder saw the dialog read the status, and no resize went out.
    expect(calls, "calls the page sent").toContain("memory.get_swap_status");
    expect(calls, "calls the page sent").not.toContain(
      "memory.resize_swap_file",
    );
    expect(await fileState(OWN)).toBe(
      `600 root root ${320 * MiB} regular file`,
    );
    expect(await procSwaps()).toEqual({ [OWN]: swapKiB(320) });
  });

  test("5. Remove warns of no swap, then takes the file, its line and its swap away", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await openOverflowDialog(page);
    await ownRow(page)
      .getByRole("button", { name: `Remove ${OWN}` })
      .click();
    const step = page.getByRole("dialog", { name: `Remove ${OWN}?` });
    await expect(step).toBeVisible();
    await expect(step.getByRole("listitem")).toHaveText([
      `${OWN}, 320 MB, is turned off, taken out of /etc/fstab and deleted.`,
      "Data parked in it is brought back to memory first.",
      "The host then has no swap. If memory runs out, Linux will close programs.",
    ]);

    const since = await vmClock();
    await recordTray(page);
    await step.getByRole("button", { name: "Remove", exact: true }).click();
    await expect(step).toBeHidden();
    await expect(overflowDialog(page)).toBeVisible();

    await expect(toastOf(page, `Removed ${OWN}`)).toBeVisible({
      timeout: 60_000,
    });
    await expect(ownRow(page)).toHaveCount(0, { timeout: 30_000 });
    await expect(
      swapFiles(page).getByText(
        "No swap is on, and no swap file is in /etc/fstab.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(
      overflowDialog(page).getByRole("button", { name: "Create a swap file" }),
    ).toBeEnabled();

    const tray = await trayNames(page);
    console.log(`tray while removing: ${tray.join(" | ")}`);
    console.log(`job host: ${await swapJournal(since)}`);
    expect
      .soft(tray, "the tray named the running job")
      .toContainEqual(
        expect.stringMatching(
          /^Open notifications: Removing (a swap file|\/var\/lib\/linuxio\/swap\/swapfile)/,
        ),
      );

    expect(await fileState(OWN)).toBe("missing");
    expect(await procSwaps()).toEqual({});
    expect(await fstabSwapLines()).toEqual([]);
  });

  test("6. a switch to compressed memory removes the file it lists, and zram swaps", async ({
    page,
  }) => {
    // stopAptRuns waits up to ten minutes for an apt run in progress, and
    // the install downloads zram-tools.
    test.setTimeout(900_000);
    await stopAptRuns();
    await vmSsh("sudo bash -s", INSTALL_ZRAM_TOOLS);
    expect(
      (await vmSsh("systemctl is-active zramswap.service || true")).trim(),
    ).toBe("inactive");
    expect(await procSwaps(), "/proc/swaps").toEqual({});

    await openOverflowDialog(page);
    const zram = overflowDialog(page).getByRole("radio", {
      name: "Compressed memory only",
    });
    await expect(
      overflowDialog(page).getByText("zram (zram-tools)", { exact: true }),
    ).toBeVisible({ timeout: 15_000 });

    // The file again, as in step 2.
    await overflowDialog(page)
      .getByRole("button", { name: "Create a swap file" })
      .click();
    const create = page.getByRole("dialog", { name: "Create a swap file" });
    await create.getByRole("spinbutton", { name: "Size in MiB" }).fill("256");
    await create.getByRole("button", { name: "Create", exact: true }).click();
    await expect(toastOf(page, "Swap file created")).toBeVisible({
      timeout: 60_000,
    });
    await expect(ownRow(page)).toContainText("256 MB", { timeout: 30_000 });
    expect(await procSwaps()).toEqual({ [OWN]: swapKiB(256) });

    await zram.check();
    await overflowDialog(page)
      .getByRole("button", { name: "Apply", exact: true })
      .click();
    const confirm = page.getByRole("dialog", {
      name: "Switch to compressed memory?",
    });
    await expect(confirm).toBeVisible();
    const items = await confirm.getByRole("listitem").allInnerTexts();
    console.log(`the switch's confirm: ${items.join(" | ")}`);
    expect(items).toContain("Compressed memory is turned on.");
    expect(items).toContain(`${OWN}, 256 MB, is removed.`);
    expect(items).toContain(
      "Data parked in a swap file is brought back to memory before the file is turned off and deleted.",
    );

    const since = await vmClock();
    await recordTray(page);
    await confirm
      .getByRole("button", { name: "Turn on compressed memory", exact: true })
      .click();
    await expect(confirm).toBeHidden();
    // Applied, applied with a warning, or failed: the toast says which.
    const ended = toastOf(
      page,
      /Memory overflow setting applied|Failed to change how memory runs out|stays on|keeps its current size|did not reach/,
    );
    await expect(ended).toBeVisible({ timeout: 120_000 });
    await expect(ended.first(), "the switch's toast").toContainText(
      "Memory overflow setting applied",
    );

    await expect(ownRow(page)).toHaveCount(0, { timeout: 30_000 });
    await expect(
      swapFiles(page).getByRole("listitem").filter({ hasText: "/dev/zram0" }),
    ).toContainText("Compressed memory");
    await expect(zram).toBeChecked();

    console.log(`tray while switching: ${(await trayNames(page)).join(" | ")}`);
    console.log(`job host: ${await swapJournal(since)}`);
    expect(await fileState(OWN)).toBe("missing");
    expect(await fstabSwapLines()).toEqual([]);
    const swaps = await procSwaps();
    expect(Object.keys(swaps), "/proc/swaps").toEqual(["/dev/zram0"]);
  });
});
