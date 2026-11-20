import { expect, type Page, test } from "@playwright/test";

import { rig, signIn, signOut, signOutIfSignedIn, vmSsh } from "./vm";

// Docker's wait for published host IPs at a real boot (docs/config-storage.md,
// "Wait for published host IPs"). Two units stand in for the two kinds of
// host: one puts an address on a bridge before the network comes up, the
// other adds an address 10 s after everything Docker's unit is ordered after,
// which is when Docker starts without the wait. One container publishes a
// port on each.
const CONTAINER = "e2e-host-ip-wait";
const EARLY = { address: "10.99.1.1", link: "e2e-br0", unit: "e2e-early" };
const LATE = { address: "10.99.0.1", link: "e2e-late", unit: "e2e-late" };
const LATE_SECONDS = 10;
const DROP_IN = "linuxio-wait-host-ips.conf";

const unitFile = (unit: string) => `/etc/systemd/system/${unit}.service`;

const EARLY_UNIT = `[Unit]
Description=LinuxIO e2e: an address on a bridge before the network is up
DefaultDependencies=no
Before=network-pre.target
Wants=network-pre.target
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/sh -c 'ip link add ${EARLY.link} type bridge && ip addr add ${EARLY.address}/24 dev ${EARLY.link} && ip link set ${EARLY.link} up'
[Install]
WantedBy=sysinit.target
`;

const LATE_UNIT = `[Unit]
Description=LinuxIO e2e: an address that arrives after Docker could start
After=network-online.target containerd.service docker.socket
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/sh -c 'sleep ${LATE_SECONDS}; ip link add ${LATE.link} type dummy && ip addr add ${LATE.address}/24 dev ${LATE.link} && ip link set ${LATE.link} up'
[Install]
WantedBy=multi-user.target
`;

const cleanUp = () =>
  vmSsh(
    `sudo docker rm -f ${CONTAINER} >/dev/null 2>&1; ` +
      `sudo systemctl disable --now ${EARLY.unit}.service ${LATE.unit}.service >/dev/null 2>&1; ` +
      `sudo rm -f ${unitFile(EARLY.unit)} ${unitFile(LATE.unit)}; sudo systemctl daemon-reload; ` +
      `sudo ip link del ${EARLY.link} 2>/dev/null; sudo ip link del ${LATE.link} 2>/dev/null; true`,
  );

test.beforeAll(async () => {
  test.setTimeout(120_000);
  await cleanUp();
  await vmSsh(`sudo tee ${unitFile(EARLY.unit)} >/dev/null`, EARLY_UNIT);
  await vmSsh(`sudo tee ${unitFile(LATE.unit)} >/dev/null`, LATE_UNIT);
  // Both addresses are on the host from here, so the container can be made.
  await vmSsh(
    `sudo systemctl daemon-reload && sudo systemctl enable --now ${EARLY.unit}.service ${LATE.unit}.service 2>/dev/null`,
  );
  await vmSsh(
    `sudo docker run -d --name ${CONTAINER} --restart always ` +
      `-p ${EARLY.address}:8081:80 -p ${LATE.address}:8080:80 busybox:1.37 sleep infinity >/dev/null`,
  );
});

test.afterAll(cleanUp);

test.afterEach(async ({ page }) => signOutIfSignedIn(page));

/** Reboots and waits until Docker is up and the late address is on the host. */
async function rebootAndSettle() {
  await rig(["reboot"]);
  await expect
    .poll(
      async () =>
        (
          await vmSsh(
            `systemctl is-active docker.service ${LATE.unit}.service; true`,
          )
        )
          .trim()
          .split("\n"),
      {
        intervals: [2_000],
        message: "Docker and the late address",
        timeout: 120_000,
      },
    )
    .toEqual(["active", "active"]);
}

const containerState = async () =>
  (
    await vmSsh(
      `sudo docker inspect -f '{{.State.Running}} {{.State.Error}}' ${CONTAINER}`,
    )
  ).trim();

interface HostIPWait {
  address: string;
  seconds: number;
  status: number;
}

/** Each wait line of Docker's unit, as systemd ran it at this boot. */
async function hostIPWaits(): Promise<HostIPWait[]> {
  const out = await vmSsh("systemctl show docker.service -p ExecStartPre");
  const line =
    /show to (\S+) -tentative.*?start_time=\[\w+ (\S+) (\S+) \w+\] ; stop_time=\[\w+ (\S+) (\S+) \w+\].*?status=(\d+)/g;
  return [...out.matchAll(line)].map((match) => ({
    address: match[1],
    seconds:
      (Date.parse(`${match[4]}T${match[5]}Z`) -
        Date.parse(`${match[2]}T${match[3]}Z`)) /
      1000,
    status: Number(match[6]),
  }));
}

/** The "Wait for published host IPs" block of the Docker settings. */
async function openSetting(page: Page) {
  await expect(async () => {
    await signIn(page);
  }).toPass({ timeout: 90_000 });
  await page.goto("/settings?tab=docker");
  const toggle = page.getByRole("checkbox", {
    name: "Wait for published host IPs",
  });
  await expect(toggle).toBeVisible({ timeout: 15_000 });
  const block = toggle.locator(
    "xpath=ancestor::div[.//button[normalize-space()='Save']][1]",
  );
  return { block, toggle };
}

const toastOf = (page: Page, text: string) =>
  page.locator("[data-sonner-toast]").filter({ hasText: text });

/** Switches the setting off on its page and checks the drop-in went. */
async function switchOff(
  page: Page,
  { block, toggle }: Awaited<ReturnType<typeof openSetting>>,
) {
  if (await toggle.isChecked()) {
    await toggle.uncheck();
    await block.getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      toastOf(page, "Docker no longer waits for host IPs."),
    ).toBeVisible({ timeout: 15_000 });
  }
  expect(await vmSsh("systemctl cat docker.service")).not.toContain(DROP_IN);
}

test("without the wait, a container publishing on a late address stays down after a boot", async ({
  page,
}) => {
  test.setTimeout(300_000);
  // A run that stopped after its save left the setting on.
  await switchOff(page, await openSetting(page));
  await signOut(page);

  await rebootAndSettle();

  expect(await containerState()).toMatch(
    new RegExp(
      `^false .*${LATE.address.replaceAll(".", "\\.")}:8080.*cannot assign requested address`,
    ),
  );
});

test("with the wait, Docker starts once the late address is there and the container runs", async ({
  page,
}) => {
  test.setTimeout(300_000);
  const { block, toggle } = await openSetting(page);
  await expect(toggle).not.toBeChecked();
  await toggle.check();
  // The list starts from the published addresses the host holds now.
  await expect
    .poll(async () =>
      (
        await block
          .getByRole("textbox")
          .evaluateAll((fields) =>
            fields.map((field) => (field as HTMLInputElement).value),
          )
      ).sort(),
    )
    .toEqual([LATE.address, EARLY.address]);
  await block.getByRole("button", { name: "Save", exact: true }).click();
  await expect(
    toastOf(page, "Docker will wait for the listed host IPs."),
  ).toBeVisible({ timeout: 15_000 });

  const unit = await vmSsh("systemctl cat docker.service");
  expect(unit).toContain(DROP_IN);
  for (const { address } of [EARLY, LATE]) {
    expect(unit).toMatch(
      new RegExp(
        `^ExecStartPre=-\\S+/timeout 30 \\S+/sh -c 'until \\S+/ip -o addr show to ${address.replaceAll(".", "\\.")} -tentative \\| grep -q \\.; do sleep 0\\.5; done'$`,
        "m",
      ),
    );
  }
  // verify exits non-zero on a line systemd cannot read.
  await vmSsh("sudo systemd-analyze verify docker.service 2>&1");
  await page.close();

  await rebootAndSettle();

  const waits = await hostIPWaits();
  console.log(JSON.stringify(waits));
  console.log(
    await vmSsh("systemd-analyze blame | grep -E 'docker.service|e2e-'"),
  );
  const early = waits.find((wait) => wait.address === EARLY.address);
  const late = waits.find((wait) => wait.address === LATE.address);
  // 0: the address was found; timeout exits 124 when its 30 s ran out.
  expect(late).toMatchObject({ status: 0 });
  expect(late?.seconds).toBeGreaterThanOrEqual(LATE_SECONDS / 2);
  expect(late?.seconds).toBeLessThan(30);
  // An address the host holds when Docker starts costs no wait.
  expect(early).toMatchObject({ status: 0 });
  expect(early?.seconds).toBeLessThanOrEqual(2);
  expect(await containerState()).toBe("true");
});

test("switched off, the drop-in is gone", async ({ page }) => {
  test.setTimeout(120_000);
  const setting = await openSetting(page);
  await expect(setting.toggle).toBeChecked();

  await switchOff(page, setting);

  await vmSsh("sudo systemd-analyze verify docker.service 2>&1");
});
