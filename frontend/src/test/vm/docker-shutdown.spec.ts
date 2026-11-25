import { expect, type Page, test } from "@playwright/test";

import { rig, signIn, signOutIfSignedIn, vmSsh } from "./vm";

// Stopping containers before shutdown at a real reboot (docs/docker-shutdown.md).
// Live restore is on, so without the unit systemd ends the containers. Three
// containers, one per restart policy that behaves differently: the
// unless-stopped one ignores SIGTERM and records the stop signal Docker sends
// it (SIGUSR1) in a folder on the host, which only a docker stop writes.
const UNIT = "linuxio-docker-shutdown.service";
const UNIT_FILE = `/etc/systemd/system/${UNIT}`;
const LIST = "/var/lib/linuxio/docker-shutdown/running";
const OUT = "/var/tmp/e2e-docker-shutdown";
const BACKUP = "/root/daemon.json.e2e-shutdown";
const CONTAINERS = {
  always: "e2e-shutdown-always",
  none: "e2e-shutdown-none",
  unlessStopped: "e2e-shutdown-unless-stopped",
};

const removeContainers = () =>
  `sudo docker rm -f ${Object.values(CONTAINERS).join(" ")} >/dev/null 2>&1; `;

test.beforeAll(async () => {
  test.setTimeout(120_000);
  await vmSsh(
    removeContainers() +
      `sudo rm -rf ${OUT} && sudo mkdir -p ${OUT} && ` +
      `sudo rm -f ${BACKUP} && { ! sudo test -f /etc/docker/daemon.json || sudo cp -a /etc/docker/daemon.json ${BACKUP}; }`,
  );
  await vmSsh(
    `sudo docker run -d --name ${CONTAINERS.unlessStopped} --restart unless-stopped ` +
      `--stop-signal USR1 -v ${OUT}:/out busybox:1.37 ` +
      `sh -c 'trap "" TERM; trap "echo usr1 > /out/signal; exit 0" USR1; while :; do sleep 1; done' >/dev/null && ` +
      `sudo docker run -d --name ${CONTAINERS.always} --restart always busybox:1.37 sleep infinity >/dev/null && ` +
      `sudo docker run -d --name ${CONTAINERS.none} busybox:1.37 sleep infinity >/dev/null`,
  );
});

// The unit goes first, so Docker's reload with the saved settings cannot
// meet a host that is shutting down.
test.afterAll(async () => {
  await vmSsh(
    removeContainers() +
      `sudo systemctl disable --now ${UNIT} >/dev/null 2>&1; sudo rm -f ${UNIT_FILE} ${LIST}; sudo systemctl daemon-reload; ` +
      `{ if sudo test -f ${BACKUP}; then sudo mv ${BACKUP} /etc/docker/daemon.json; else sudo rm -f /etc/docker/daemon.json; fi; } && ` +
      `sudo systemctl reload docker.service; sudo rm -rf ${OUT}; true`,
  );
});

test.afterEach(async ({ page }) => signOutIfSignedIn(page));

/** The Docker Daemon section of the Docker settings. */
async function openDaemonSettings(page: Page) {
  await expect(async () => {
    await signIn(page);
  }).toPass({ timeout: 90_000 });
  await page.goto("/settings?tab=docker");
  const liveRestore = page.getByRole("checkbox", { name: "Live restore" });
  await expect(liveRestore).toBeVisible({ timeout: 15_000 });
  const section = liveRestore.locator(
    "xpath=ancestor::div[.//button[normalize-space()='Save']][1]",
  );
  const stop = page.getByRole("checkbox", {
    name: "Stop containers before shutdown",
  });
  const save = async () => {
    await section.getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      page
        .locator("[data-sonner-toast]")
        .filter({ hasText: "Docker daemon settings applied." }),
    ).toBeVisible({ timeout: 30_000 });
  };
  return { liveRestore, save, stop };
}

const unitState = async () =>
  (
    await vmSsh(
      `systemctl is-enabled ${UNIT} 2>/dev/null; systemctl is-active ${UNIT}; true`,
    )
  )
    .trim()
    .split("\n");

const running = async (name: string) =>
  (await vmSsh(`sudo docker inspect -f '{{.State.Running}}' ${name}`)).trim();

test("switched on, the unit is written, enabled and started", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const { liveRestore, save, stop } = await openDaemonSettings(page);
  if (!(await liveRestore.isChecked())) {
    await expect(stop).toBeDisabled();
    await liveRestore.check();
  }
  await expect(stop).not.toBeChecked();
  await stop.check();
  await save();

  expect(await unitState()).toEqual(["enabled", "active"]);
  // verify exits non-zero on a line systemd cannot read.
  await vmSsh(`sudo systemd-analyze verify ${UNIT_FILE} 2>&1`);
  expect(await vmSsh(`systemctl show ${UNIT} -p WantedBy --value`)).toContain(
    "docker.service",
  );
  // A stop of the unit by hand, outside a shutdown, leaves the containers.
  await vmSsh(`sudo systemctl restart ${UNIT}`);
  expect(await running(CONTAINERS.unlessStopped)).toBe("true");
  expect(await vmSsh(`sudo test -e ${LIST} && echo listed; true`)).toBe("");
});

test("at a reboot, Docker stops the containers and each policy comes back as without live restore", async ({
  page,
}) => {
  test.setTimeout(300_000);
  await page.close();

  await rig(["reboot"]);
  await expect
    .poll(
      async () =>
        (await vmSsh(`systemctl is-active docker.service ${UNIT}; true`))
          .trim()
          .split("\n"),
      { intervals: [2_000], message: "Docker and the unit", timeout: 180_000 },
    )
    .toEqual(["active", "active"]);

  // Only docker stop sends USR1: systemd would have sent TERM, then KILL.
  expect((await vmSsh(`sudo cat ${OUT}/signal 2>/dev/null; true`)).trim()).toBe(
    "usr1",
  );
  expect(await running(CONTAINERS.unlessStopped)).toBe("true");
  expect(await running(CONTAINERS.always)).toBe("true");
  expect(await running(CONTAINERS.none)).toBe("false");
  expect(await vmSsh(`sudo test -e ${LIST} && echo listed; true`)).toBe("");
  console.log(
    await vmSsh(
      `sudo journalctl -b -u ${UNIT} --no-pager -o cat 2>&1 | tail -20`,
    ),
  );
});

test("switched off, the unit is gone and the containers keep running", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const { save, stop } = await openDaemonSettings(page);
  await expect(stop).toBeChecked();
  await stop.uncheck();
  await save();

  expect(await vmSsh(`sudo test -e ${UNIT_FILE} && echo present; true`)).toBe(
    "",
  );
  expect((await unitState())[0]).not.toBe("enabled");
  expect(await running(CONTAINERS.unlessStopped)).toBe("true");
  expect(await running(CONTAINERS.always)).toBe("true");
});
