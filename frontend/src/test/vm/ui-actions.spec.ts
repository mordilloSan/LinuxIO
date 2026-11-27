import { execFile } from "node:child_process";
import { randomBytes } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { expect, test, type Locator, type Page } from "@playwright/test";

import {
  bridgeCall,
  type Login,
  signIn,
  signOut,
  signOutIfSignedIn,
  stopAptRuns,
  vmSsh,
} from "./vm";

// Four things no browser test had clicked: a session without sudo, the
// "Open the real folder" action of a file operation's error toast, the page
// link of a toast fired on another page, and the navbar's Reboot and Power
// Down. They are in one file so that the last two, which restart the VM,
// run after the others (the specs of a file run in order, one worker).
//
// Run `make vm-reset` and `make vm-install` before a run; the checks leave
// the VM running and signed out, with the apt timers stopped until its next
// boot.

const NO_SUDO_USER = "e2e-ui";
const LINK_DIR = "/srv/e2e-link";
const BOOT_ID = "cat /proc/sys/kernel/random/boot_id";

test.afterEach(async ({ page }) => {
  // A test that failed with the page on an overlay or on the sign-in page
  // has nothing to sign out of.
  await signOutIfSignedIn(page).catch(() => undefined);
});

const toastOf = (page: Page, text: string | RegExp) =>
  page.locator("[data-sonner-toast]").filter({ hasText: text });

const bootId = async () => (await vmSsh(BOOT_ID)).trim();

/** A user without sudo, with a password of this run; its home holds a file. */
async function createNoSudoUser(): Promise<Login> {
  const login = {
    username: NO_SUDO_USER,
    password: randomBytes(18).toString("base64url"),
  };
  await vmSsh(
    `id -u ${NO_SUDO_USER} >/dev/null 2>&1 || sudo useradd -m -s /bin/bash ${NO_SUDO_USER}`,
  );
  await vmSsh("sudo chpasswd", `${login.username}:${login.password}\n`);
  expect(await vmSsh(`id -nG ${NO_SUDO_USER}`)).not.toMatch(/\bsudo\b/);
  await vmSsh(
    `sudo -u ${NO_SUDO_USER} touch /home/${NO_SUDO_USER}/e2e-note.txt`,
  );
  return login;
}

/** Removes the user, which may still own a bridge a minute after its sign-out. */
async function removeNoSudoUser() {
  await vmSsh(
    `sudo pkill -KILL -u ${NO_SUDO_USER} || true; sudo userdel -rf ${NO_SUDO_USER} || true`,
  );
}

// 1. A session without sudo. docs/privilege_pattern.md ("In the frontend")
// lists what a session without sudo is not offered. The same checks run for
// the test user, who can sudo, so a control that is absent for the other
// session is shown to be one the page has (not a locator that matches
// nothing).

const MENU_ONLY_FOR_ADMIN = ["Reboot", "Power Down"];
const SETTINGS_ONLY_FOR_ADMIN = ["Indexer", "Monitoring", "Notifications"];

/** Expects locator to be on the page for an admin, and not for anyone else. */
async function expectOnlyFor(locator: Locator, admin: boolean) {
  if (admin) {
    await expect(locator).toBeVisible({ timeout: 15_000 });
  } else {
    await expect(locator).toHaveCount(0);
  }
}

async function checkSurface(page: Page, admin: boolean, ownName: string) {
  // The account menu: Reboot and Power Down need control.reboot and
  // control.power_off.
  await page.goto("/");
  await page.getByRole("button", { name: "Account" }).click();
  await expect(page.getByRole("menuitem", { name: "Sign out" })).toBeVisible({
    timeout: 15_000,
  });
  for (const name of MENU_ONLY_FOR_ADMIN) {
    await expectOnlyFor(page.getByRole("menuitem", { name }), admin);
  }
  await page.keyboard.press("Escape");

  // The Services page has no Scheduled tasks tab (schedules.list is
  // privileged), and the page itself is not found.
  await page.goto("/services");
  await expect(
    page.getByRole("tab", { name: "Timers", exact: true }),
  ).toBeVisible({ timeout: 15_000 });
  await expectOnlyFor(
    page.getByRole("tab", { name: "Scheduled tasks", exact: true }),
    admin,
  );
  await page.goto("/services/schedules");
  if (admin) {
    await expect(page.getByRole("button", { name: "Create task" })).toBeVisible(
      { timeout: 15_000 },
    );
  } else {
    await expect(page.getByText("Page not found.")).toBeVisible({
      timeout: 15_000,
    });
    await expect(page.getByRole("button", { name: "Create task" })).toHaveCount(
      0,
    );
  }

  // Settings: the tabs whose reads are all privileged are hidden, and a link
  // to one falls back to General.
  await page.goto("/settings?tab=monitoring");
  await expect(
    page.getByRole("tab", { name: "General", exact: true }),
  ).toBeVisible({ timeout: 15_000 });
  for (const name of SETTINGS_ONLY_FOR_ADMIN) {
    await expectOnlyFor(page.getByRole("tab", { name, exact: true }), admin);
  }
  await expect(
    page.getByRole("tab", {
      name: admin ? "Monitoring" : "General",
      exact: true,
    }),
  ).toHaveAttribute("aria-selected", "true");

  // Accounts: the lists stay for everyone, adding a user does not.
  await page.goto("/accounts");
  await expect(page.getByText(ownName, { exact: true }).first()).toBeVisible({
    timeout: 30_000,
  });
  await expectOnlyFor(page.getByRole("button", { name: "Add User" }), admin);

  // The file browser: search and Index filesystem read the root indexer.
  await page.goto("/filebrowser/tmp");
  await expect(
    // An admin's button also says what the Trash takes: "Trash, 34 GB".
    page.getByRole("button", { name: /^Trash(,|$)/ }),
  ).toBeVisible({ timeout: 15_000 });
  await expectOnlyFor(
    page.getByPlaceholder("Search files and folders..."),
    admin,
  );
  await expectOnlyFor(
    page.getByRole("button", { name: "Index filesystem" }),
    admin,
  );
}

test("a session without sudo is offered no privileged control and is refused by the routes", async ({
  page,
}) => {
  test.setTimeout(300_000);
  const login = await createNoSudoUser();
  try {
    await signIn(page);
    await checkSurface(page, true, NO_SUDO_USER);
    await signOut(page);

    await signIn(page, login);
    await checkSurface(page, false, NO_SUDO_USER);

    // What the page does not offer, the route refuses before it runs: a read
    // (schedules.list), a change (schedules.create, which would answer 400
    // for this empty request if it ran) and a Docker write.
    for (const [route, request] of [
      ["schedules.list", {}],
      ["schedules.create", {}],
      ["docker.start_container", { containerId: "e2e-none" }],
    ] as const) {
      expect(await bridgeCall(page, route, request), route).toEqual({
        status: "error",
        code: 403,
        error: expect.stringContaining(
          `privileged route ${route} requires elevated bridge`,
        ),
      });
    }

    // What needs no sudo still works: the user's own files.
    await page.goto(`/filebrowser/home/${NO_SUDO_USER}`);
    await expect(page.getByText("e2e-note.txt", { exact: true })).toBeVisible({
      timeout: 30_000,
    });
    await signOut(page);
  } finally {
    await removeNoSudoUser();
  }
});

// 2. "Open the real folder". A folder of the non-sudo user holds a link that
// user made to another folder of theirs. A root bridge, which the test user's
// session has, refuses a change below it (docs/privilege_pattern.md, "Root
// acting in the session user's folders"), and the error toast offers the real
// folder.

const LINK = `${LINK_DIR}/link`;
const REAL = `${LINK_DIR}/real`;
const AT_REAL = /\/filebrowser\/srv\/e2e-link\/real\/?$/;

async function plantLink() {
  await createNoSudoUser();
  await vmSsh(
    `sudo rm -rf ${LINK_DIR} && sudo install -d -o ${NO_SUDO_USER} -g ${NO_SUDO_USER} -m 0755 ${LINK_DIR} && ` +
      `sudo -u ${NO_SUDO_USER} sh -c 'mkdir ${REAL} && touch ${REAL}/a.txt && ln -s ${REAL} ${LINK}'`,
  );
}

async function removeLink() {
  await vmSsh(`sudo rm -rf ${LINK_DIR}`);
  await removeNoSudoUser();
}

/** Opens the folder through the link, and right-clicks its one file. */
async function openFileMenuBelowLink(page: Page) {
  await page.goto(`/filebrowser${LINK}`);
  const file = page.getByText("a.txt", { exact: true });
  await expect(file).toBeVisible({ timeout: 30_000 });
  await file.click({ button: "right" });
}

test("a file operation refused at a link offers the real folder, and the action opens it", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await plantLink();
  try {
    await signIn(page);
    await openFileMenuBelowLink(page);
    await page
      .getByRole("menuitem", { name: "New folder", exact: true })
      .click();

    const toast = toastOf(page, `${LINK} is a link to ${REAL}`);
    await expect(toast).toBeVisible({ timeout: 30_000 });
    await toast
      .getByRole("button", { name: "Open the real folder" })
      .click({ timeout: 10_000 });

    await expect(page).toHaveURL(AT_REAL, { timeout: 15_000 });
    await expect(page.getByText("a.txt", { exact: true })).toBeVisible({
      timeout: 15_000,
    });
    // Nothing was made behind the link.
    expect((await vmSsh(`ls ${REAL}`)).trim()).toBe("a.txt");
    await signOut(page);
  } finally {
    await removeLink();
  }
});

test("the real folder action closes the Permissions dialog it was raised in", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await plantLink();
  try {
    await signIn(page);
    // The link itself: a chmod follows a link at the item, under the same
    // rule. A file below the link is refused the same way.
    await page.goto(`/filebrowser${LINK_DIR}`);
    await page.getByText("link", { exact: true }).click({ button: "right" });
    await page.getByRole("menuitem", { name: "Change Permissions" }).click();
    const dialog = page
      .getByRole("dialog")
      .filter({ hasText: "Change Permissions" });
    await expect(dialog).toBeVisible({ timeout: 15_000 });
    await dialog.getByRole("button", { name: "Apply" }).click();

    // A change of one item that fails is a batch with one failure, named in
    // the toast's description, so the dialog stays open under the toast.
    const toast = toastOf(page, "Failed to change permissions");
    await expect(toast).toBeVisible({ timeout: 30_000 });
    await expect(toast).toContainText(`${LINK} is a link to ${REAL}`);
    await expect(dialog).toBeVisible();
    await toast
      .getByRole("button", { name: "Open the real folder" })
      .click({ timeout: 10_000 });

    await expect(dialog).toBeHidden({ timeout: 15_000 });
    await expect(page).toHaveURL(AT_REAL, { timeout: 15_000 });
    await signOut(page);
  } finally {
    await removeLink();
  }
});

// 3. A toast's page link (docs/api-contract.md, "Toast links"). Stopping a
// container whose process ignores SIGTERM takes Docker 10 s, and the toast
// that ends it carries "Open Docker" (DOCKER_TOAST_META). The button is there
// only when the user is on another page as the toast fires.

const STOP_ON_PAGE = "e2e-toast-on-page";
const STOP_AWAY = "e2e-toast-away";

test("a toast fired on another page links back to its page, and not on its own page", async ({
  page,
}) => {
  test.setTimeout(300_000);
  // A pull stalled on the registry would otherwise use up the test's
  // timeout with no step to blame; timeout(1) exits 124 when it cuts one.
  await vmSsh(
    `sudo docker rm -f ${STOP_ON_PAGE} ${STOP_AWAY} >/dev/null 2>&1; ` +
      `{ sudo timeout 120 docker pull -q busybox:1.37 >/dev/null || { s=$?; [ $s -ne 124 ] || echo "docker pull busybox:1.37 took more than 120 s" >&2; exit $s; }; } && ` +
      `for n in ${STOP_ON_PAGE} ${STOP_AWAY}; do sudo docker run -d --name $n busybox:1.37 sleep 3600 >/dev/null; done`,
  );
  // The container's Stop button, in a card or in a table row.
  const stopButton = (name: string) =>
    page
      .getByRole("button", { name: `Stop ${name}`, exact: true })
      .or(
        page
          .getByRole("row")
          .filter({ hasText: name })
          .getByRole("button", { name: "Stop", exact: true }),
      );
  try {
    await signIn(page);
    await page.goto("/docker/containers");

    // On the Docker page: the toast has no button.
    await stopButton(STOP_ON_PAGE).click({ timeout: 30_000 });
    const onPage = toastOf(page, `Container ${STOP_ON_PAGE} stopped`);
    await expect(onPage).toBeVisible({ timeout: 60_000 });
    await expect(
      onPage.getByRole("button", { name: "Open Docker" }),
    ).toHaveCount(0);

    // On Settings by the time it fires: the toast offers Docker.
    await stopButton(STOP_AWAY).click({ timeout: 30_000 });
    await page
      .getByLabel("Primary navigation")
      .getByRole("link", { name: "Settings", exact: true })
      .click({ timeout: 10_000 });
    await expect(page).toHaveURL(/\/settings/);
    const away = toastOf(page, `Container ${STOP_AWAY} stopped`);
    await expect(away).toBeVisible({ timeout: 60_000 });
    await away
      .getByRole("button", { name: "Open Docker" })
      .click({ timeout: 10_000 });
    await expect(page).toHaveURL(/\/docker$/, { timeout: 15_000 });
    await signOut(page);
  } finally {
    await vmSsh(
      `sudo docker rm -f ${STOP_ON_PAGE} ${STOP_AWAY} >/dev/null 2>&1 || true`,
    );
  }
});

// 4. Reboot and Power Down from the navbar. The VM is rebooted by the guest,
// so the rig only watches: `ssh` waits for the guest, and starts a domain that
// is shut off, which is how the VM comes back from a power-off.

const rigPath = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../packaging/vm/linuxio-vm.sh",
);
const execFileAsync = promisify(execFile);
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * The VM's boot ID once it answers from a boot other than previous, starting
 * the domain if it is shut off. The rig's ssh waits up to 300 s for a guest
 * that is still shutting down, and does not start a domain that powers off
 * meanwhile, so each try is cut short and made again.
 */
async function bootIdAfter(previous: string, timeout: number) {
  const deadline = Date.now() + timeout;
  for (;;) {
    try {
      const { stdout } = await execFileAsync(rigPath, ["ssh", BOOT_ID], {
        encoding: "utf8",
        killSignal: "SIGKILL",
        timeout: 45_000,
      });
      const id = stdout.trim();
      if (id !== "" && id !== previous) return id;
    } catch {
      // The guest is down, or the try was cut short.
    }
    if (Date.now() > deadline) {
      throw new Error(`the VM did not boot again within ${timeout / 1000} s`);
    }
    await sleep(3_000);
  }
}

const webserverAnswers = (page: Page) =>
  page.evaluate(async () => {
    try {
      const response = await fetch("/api/version", {
        cache: "no-store",
        signal: AbortSignal.timeout(5_000),
      });
      return response.ok;
    } catch {
      return false;
    }
  });

/** Signs in again once the host is back: the sign-in may wait for PAM and the session units. */
async function signInWhenUp(page: Page) {
  await expect(async () => {
    await page.goto("/sign-in");
    await signIn(page);
  }).toPass({ intervals: [5_000], timeout: 180_000 });
}

async function confirmFromAccountMenu(
  page: Page,
  item: "Reboot" | "Power Down",
) {
  await page.getByRole("button", { name: "Account" }).click();
  await page.getByRole("menuitem", { name: item, exact: true }).click();
  const dialog = page
    .getByRole("dialog")
    .filter({ hasText: `Confirm ${item}` });
  await expect(dialog).toBeVisible({ timeout: 10_000 });
  await dialog.getByRole("button", { name: item, exact: true }).click();
}

const REBOOT_TEXT =
  "The reboot was requested. You will be redirected to sign in once the server is back online.";
const WAITING_TEXT =
  "The power-off was requested. Waiting for the host to answer.";
const POWERED_OFF_TEXT =
  "The system is shutting down. You may close this window.";

test("Reboot shows the overlay until the VM is back, then sends the user to sign in", async ({
  page,
}) => {
  test.setTimeout(900_000);
  await stopAptRuns();
  const before = await bootId();
  // The overlay's polls of /api/version, in the order the page saw them.
  const polls: string[] = [];
  page.on("response", (response) => {
    if (response.url().endsWith("/api/version")) {
      polls.push(String(response.status()));
    }
  });
  page.on("requestfailed", (request) => {
    if (request.url().endsWith("/api/version")) polls.push("failed");
  });

  await signIn(page);
  await confirmFromAccountMenu(page, "Reboot");
  await expect(page.getByText("Rebooting...", { exact: true })).toBeVisible({
    timeout: 10_000,
  });
  await expect(page.getByText(REBOOT_TEXT)).toBeVisible();

  // The webserver is seen going away while the overlay still holds the page.
  await expect
    .poll(() => polls.includes("failed"), {
      intervals: [1_000],
      message: "a poll of /api/version that failed",
      timeout: 120_000,
    })
    .toBe(true);
  await expect(page).not.toHaveURL(/\/sign-in/);
  await expect(page.getByText("Rebooting...", { exact: true })).toBeVisible();

  // Only then does it leave: to sign in, once the host answers again.
  await expect(page).toHaveURL(/\/sign-in/, { timeout: 300_000 });
  expect(polls.indexOf("failed")).toBeLessThan(polls.lastIndexOf("200"));
  // The guest really restarted: a boot ID other than the one it had.
  expect(await bootIdAfter(before, 300_000)).not.toBe(before);
  await stopAptRuns();

  await signInWhenUp(page);
  await signOut(page);
});

test("Power Down says the host is shutting down once it answered, and the VM can be started again", async ({
  page,
}) => {
  test.setTimeout(900_000);
  await stopAptRuns();
  const before = await bootId();

  await signIn(page);
  // The texts the overlay shows, in order: the page does not reload, so the
  // observer lives until the VM is back.
  await page.evaluate(
    ([waiting, off, unknown]) => {
      const seen: string[] = [];
      (window as unknown as { __overlay: string[] }).__overlay = seen;
      new MutationObserver(() => {
        const text = document.body.innerText;
        for (const known of [waiting, off, unknown]) {
          if (text.includes(known) && !seen.includes(known)) seen.push(known);
        }
      }).observe(document.body, {
        characterData: true,
        childList: true,
        subtree: true,
      });
    },
    [WAITING_TEXT, POWERED_OFF_TEXT, "The host did not answer."],
  );
  await confirmFromAccountMenu(page, "Power Down");
  await expect(page.getByText("Shutting Down...", { exact: true })).toBeVisible(
    { timeout: 10_000 },
  );
  await expect(page.getByText(POWERED_OFF_TEXT)).toBeVisible({
    timeout: 60_000,
  });
  expect(
    await page.evaluate(
      () => (window as unknown as { __overlay: string[] }).__overlay,
    ),
    "the overlay's texts, in order",
  ).toEqual([WAITING_TEXT, POWERED_OFF_TEXT]);

  // The host really goes: the webserver stops answering.
  await expect
    .poll(() => webserverAnswers(page), {
      intervals: [2_000],
      message: "the webserver stopped answering",
      timeout: 180_000,
    })
    .toBe(false);

  // Start the domain again, wait for the guest, and for the webserver.
  expect(await bootIdAfter(before, 400_000)).not.toBe(before);
  await stopAptRuns();
  await expect
    .poll(() => webserverAnswers(page), {
      intervals: [3_000],
      message: "the webserver answers again",
      timeout: 180_000,
    })
    .toBe(true);
  await signInWhenUp(page);
  await signOut(page);
});
