import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";

import { expect, test, type Locator, type Page } from "@playwright/test";

import {
  fixture,
  jobIds,
  jobRecords,
  loggedJobs,
  MiB,
  openTray,
  rig,
  SCRUB,
  signIn,
  signOut,
  signOutIfSignedIn,
  startScrub,
  trayJob,
  vmSsh,
  waitForJob,
} from "./vm";

// Phase 2 of docs/TODO/jobs.md: every mutation runs as a job in the root job
// host (the test user can sudo, so its login is privileged). Each job here is
// started in the browser; the VM's job store and journal are the evidence.

const HOME = "/home/linuxio-e2e";

// Each login holds one of linuxio-auth.socket's 16 connections until it signs
// out; a closed browser does not sign out.
test.afterEach(({ page }) => signOutIfSignedIn(page));

test("a reload mid package update picks the same job up and completes it", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await vmSsh("sudo bash -s", await readFile(fixture("slow-package.sh")));
  const before = await jobIds();

  await signIn(page);
  await page.goto("/updates");
  const card = page
    .locator("div")
    .filter({ has: page.getByText("linuxio-e2e-slow · 2.0") })
    .filter({ has: page.getByRole("button", { name: "Update", exact: true }) })
    .last();
  await card.getByRole("button", { name: "Update", exact: true }).click();
  await waitForJob("packages.update", before, "running");

  // The postinst sleeps 20 s, so the job is still running after the reload.
  // PackageKit answers the page's update list only after the transaction, so
  // the tray is what shows the job picked up again.
  await page.reload();
  const tray = await openTray(page);
  const item = trayJob(tray, /linuxio-e2e-slow|Updating packages/);
  await expect(item).toBeVisible({ timeout: 15_000 });

  await waitForJob("packages.update", before, "completed");
  await expect(item).toBeHidden({ timeout: 15_000 });
  await page.keyboard.press("Escape");
  await expect(page.getByText("linuxio-e2e-slow · 2.0")).toBeHidden({
    timeout: 30_000,
  });
  expect(await vmSsh("dpkg-query -W -f '${Version}' linuxio-e2e-slow")).toBe(
    "2.0",
  );
});

const DOCKER_DIR = `${HOME}/docker`;

/**
 * Removes the stack's containers and folder. Saving names the fixture's
 * containers after its services, so the containers of every earlier stack go
 * too. The VM's clean snapshot has docker.io without the Compose plugin and no
 * Docker folder, so this adds both, and pulls the fixture's image.
 */
async function resetStack(name: string) {
  await vmSsh(
    `{ sudo docker compose version >/dev/null 2>&1 || sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq docker-compose-v2 >/dev/null; } && ` +
      `mkdir -p ${DOCKER_DIR} && sudo docker pull -q busybox:1.37 >/dev/null && ` +
      `{ sudo docker compose -p ${name} down -t 0 >/dev/null 2>&1 || true; } && ` +
      `{ sudo docker rm -f app slow >/dev/null 2>&1 || true; } && ` +
      `sudo rm -rf ${DOCKER_DIR}/${name}`,
  );
}

/** Creates a stack from the slow Compose fixture through Create Stack; stops at Save. */
async function saveStack(page: Page, name: string) {
  await page.goto("/docker/compose");
  await page.getByRole("button", { name: "Create Stack" }).click();
  const setup = page.getByRole("dialog", {
    name: "Create New Docker Compose Stack",
  });
  await setup.getByLabel("Stack Name").fill(name);
  await setup.getByRole("button", { name: "Next" }).click();
  const editor = page.getByRole("dialog", { name: "Docker Compose editor" });
  await editor.getByRole("textbox", { name: "Code editor" }).first().click();
  await page.keyboard.insertText(
    await readFile(fixture("compose-slow.yml"), "utf8"),
  );
  await editor.getByRole("button", { name: "Save", exact: true }).click();
}

/** Saves the stack and starts it from the post-save dialog. */
async function startStack(page: Page, name: string) {
  await saveStack(page, name);
  await page
    .getByRole("dialog", { name: "Stack Saved Successfully" })
    .getByRole("button", { name: "Start Stack" })
    .click();
  await expect(page.getByText(`Starting Stack: ${name}`)).toBeVisible();
}

// The tray labels a Compose job with its latest progress line.
const COMPOSE_ITEM = /compose|Container (app|slow)/i;

/** Both fixture services run, "slow" healthy. */
async function expectStackUp(name: string) {
  const ps = await vmSsh(
    `sudo docker ps --filter label=com.docker.compose.project=${name} --format '{{.Label "com.docker.compose.service"}} {{.Status}}'`,
  );
  expect(ps).toMatch(/^app Up/m);
  expect(ps).toMatch(/^slow Up .*\(healthy\)/m);
}

test("a reload mid Compose up picks the job up and completes it", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const name = "e2e-reload";
  await resetStack(name);
  const before = await jobIds();

  await signIn(page);
  await startStack(page, name);
  await waitForJob("docker.compose", before, "running");

  await page.reload();
  const tray = await openTray(page);
  const item = trayJob(tray, COMPOSE_ITEM);
  await expect(item).toBeVisible({ timeout: 15_000 });

  await waitForJob("docker.compose", before, "completed");
  await expect(item).toBeHidden({ timeout: 15_000 });
  await expectStackUp(name);
});

test("closing the Compose dialog mid-run leaves the job running to completion", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const name = "e2e-close";
  await resetStack(name);
  const before = await jobIds();

  await signIn(page);
  await startStack(page, name);
  await waitForJob("docker.compose", before, "running");

  await page
    .getByRole("button", { name: "Close compose operation dialog" })
    .click();
  await expect(
    page.getByText("Compose operation is still running in the background"),
  ).toBeVisible();
  await expect(page.getByText(`Starting Stack: ${name}`)).toBeHidden();
  const tray = await openTray(page);
  const item = trayJob(tray, COMPOSE_ITEM);
  await expect(item).toBeVisible({ timeout: 15_000 });

  await waitForJob("docker.compose", before, "completed");
  await expect(item).toBeHidden({ timeout: 15_000 });
  await expectStackUp(name);
});

test("saving a Compose stack over an existing file asks to overwrite it", async ({
  page,
}) => {
  const name = "e2e-overwrite";
  const file = `${DOCKER_DIR}/${name}/docker-compose.yml`;
  await resetStack(name);
  await vmSsh(
    `mkdir -p ${DOCKER_DIR}/${name} && printf '# the file to overwrite\\nservices: {}\\n' > ${file}`,
  );

  await signIn(page);
  await saveStack(page, name);
  const prompt = page.getByRole("dialog", { name: "File Already Exists" });
  await expect(prompt).toContainText(file);
  await prompt.getByRole("button", { name: "Overwrite" }).click();
  await page
    .getByRole("dialog", { name: "Stack Saved Successfully" })
    .getByRole("button", { name: "Do Nothing" })
    .click();

  const saved = await vmSsh(`cat ${file}`);
  expect(saved).not.toContain("# the file to overwrite");
  expect(saved).toContain("service_healthy");
});

test("a reload mid file copy picks the job up and completes it", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const dir = `${HOME}/e2e-copy`;
  // dd writes real blocks, which fallocate does not, and the job host's reads
  // are capped at 100 MB/s, so the 2 GiB copy takes about 20 s.
  await vmSsh(
    `sudo rm -rf ${dir} && mkdir -p ${dir}/dst && ` +
      `dd if=/dev/zero of=${dir}/big.bin bs=1M count=2048 status=none && sync && ` +
      `echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null && ` +
      `sudo systemctl set-property --runtime linuxio-jobs@0.service 'IOReadBandwidthMax=/dev/vda 100M'`,
  );
  try {
    const before = await jobIds();
    await signIn(page);
    await page.goto(`/filebrowser${dir}`);
    await page.getByText("big.bin", { exact: true }).click();
    await page.keyboard.press("Control+c");
    await page.getByText("dst", { exact: true }).dblclick();
    await expect(page.getByText("This folder is empty.")).toBeVisible();
    await page.keyboard.press("Control+v");
    await waitForJob("filebrowser.copy_batch", before, "running");

    await page.reload();
    const tray = await openTray(page);
    const item = trayJob(tray, "Copying");
    await expect(item).toBeVisible({ timeout: 15_000 });

    await waitForJob("filebrowser.copy_batch", before, "completed", {
      timeout: 120_000,
    });
    await expect(item).toBeHidden({ timeout: 15_000 });
    expect(
      await vmSsh(
        `cmp ${dir}/big.bin ${dir}/dst/big.bin && stat -c %s ${dir}/dst/big.bin`,
      ),
    ).toBe("2147483648\n");
  } finally {
    await vmSsh(
      `sudo systemctl set-property --runtime linuxio-jobs@0.service IOReadBandwidthMax= && sudo rm -rf ${dir}`,
    );
  }
});

const sha256 = async (file: string) =>
  createHash("sha256")
    .update(await readFile(file))
    .digest("hex");

/** Uploads files through the context menu's Upload dialog. */
async function upload(page: Page, rightClick: Locator, files: string[]) {
  await rightClick.click({ button: "right" });
  await page.getByRole("menuitem", { name: "Upload" }).click();
  const dialog = page.getByRole("dialog", { name: "Upload files or folders" });
  const chooser = page.waitForEvent("filechooser");
  await dialog.getByRole("button", { name: "Select files" }).click();
  await (await chooser).setFiles(files);
  await dialog.getByRole("button", { name: "Upload", exact: true }).click();
  await expect(dialog).toBeHidden();
}

test("upload, batch upload and archive download complete with matching files", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const dir = `${HOME}/e2e-upload`;
  const names = ["single.txt", "batch-a.txt", "batch-b.txt"];
  const sums = Object.fromEntries(
    await Promise.all(
      names.map(async (name) => [
        name,
        await sha256(fixture(`upload/${name}`)),
      ]),
    ),
  );
  await vmSsh(`sudo rm -rf ${dir} && mkdir -p ${dir}/batch`);
  const before = await jobIds();

  await signIn(page);
  await page.goto(`/filebrowser${dir}`);
  await upload(page, page.getByText("batch", { exact: true }), [
    fixture("upload/single.txt"),
  ]);
  await expect(page.getByText(`Uploaded 1 item to ${dir}`)).toBeVisible();

  await page.getByText("batch", { exact: true }).dblclick();
  await upload(page, page.getByText("This folder is empty."), [
    fixture("upload/batch-a.txt"),
    fixture("upload/batch-b.txt"),
  ]);
  await expect(
    page.getByText(`Uploaded 2 items to ${dir}/batch`),
  ).toBeVisible();

  const onVm = await vmSsh(
    `cd ${dir} && sha256sum single.txt batch/batch-a.txt batch/batch-b.txt`,
  );
  expect(onVm).toBe(
    `${sums["single.txt"]}  single.txt\n` +
      `${sums["batch-a.txt"]}  batch/batch-a.txt\n` +
      `${sums["batch-b.txt"]}  batch/batch-b.txt\n`,
  );

  // A folder downloads as a zip the archive job builds.
  await page.goto(`/filebrowser${dir}`);
  await page.getByText("batch", { exact: true }).click({ button: "right" });
  const downloading = page.waitForEvent("download");
  await page.getByRole("menuitem", { name: "Download" }).click();
  const download = await downloading;
  expect(download.suggestedFilename()).toBe("batch.zip");
  const zip = await readFile(await download.path());
  const unpacked = await vmSsh(
    "rm -rf /tmp/e2e-zip && mkdir /tmp/e2e-zip && cd /tmp/e2e-zip && cat > batch.zip && " +
      "python3 -m zipfile -e batch.zip out && cd out && find . -type f -printf '%f ' -exec sha256sum {} \\; | awk '{print $1, $2}' | sort",
    zip,
  );
  expect(unpacked).toBe(
    `batch-a.txt ${sums["batch-a.txt"]}\nbatch-b.txt ${sums["batch-b.txt"]}\n`,
  );

  // Jobs under a second leave no record (jobs.md 2.10); none may fail.
  const records = (await jobRecords()).filter(
    (r) =>
      !before.has(r.id) &&
      ["filebrowser.upload_batch", "filebrowser.archive"].includes(r.route),
  );
  expect(records.filter((r) => r.state !== "completed")).toEqual([]);
});

test("a btrfs scrub keeps running across logout and login", async ({
  page,
}) => {
  test.setTimeout(360_000);
  const before = await jobIds();
  const { uuid } = await startScrub(page, before, 8 * MiB);
  await expect(page.getByText(/Checking for errors/)).toBeVisible({
    timeout: 20_000,
  });

  await signOut(page);
  await signIn(page);
  await page.goto(`/storage?pool=${uuid}`);
  // The banner prints the kernel's percentage unrounded (18.98...%).
  await expect(page.getByText(/Checking for errors · [\d.]+%/)).toBeVisible({
    timeout: 20_000,
  });
  await waitForJob(SCRUB, before, "running");

  await waitForJob(SCRUB, before, "completed", { timeout: 240_000 });
  await expect(page.getByText(/Last check: .*, 0 errors/)).toBeVisible({
    timeout: 20_000,
  });
});

// Host events (docs/TODO/jobs.md section 7). Each acts on the VM only.

test("stopping the root job host marks the job interrupted/shutdown with an alert", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const before = await jobIds();
  await startScrub(page, before, 8 * MiB);

  await vmSsh("sudo systemctl stop linuxio-jobs@0.service");
  await waitForJob(SCRUB, before, "interrupted", { reason: "shutdown" });
  // The tray polls alerts once a minute; a reload reads them now.
  await page.reload();
  const tray = await openTray(page);
  await expect(
    tray
      .getByRole("listitem")
      .filter({ hasText: `${SCRUB} was interrupted` })
      .filter({ hasText: "(shutdown)" })
      .first(),
  ).toBeVisible({ timeout: 30_000 });
});

test("killing the root job host marks the job interrupted/crash", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const before = await jobIds();
  await startScrub(page, before, 8 * MiB);

  await vmSsh(
    'sudo kill -9 "$(systemctl show -P MainPID linuxio-jobs@0.service)"',
  );
  // The next host, started by the bridge's reconnect, marks it.
  await waitForJob(SCRUB, before, "interrupted", {
    reason: "crash",
    timeout: 90_000,
  });
});

test("after an orderly reboot the root job host is started at boot and resumes the scrub", async ({
  page,
}) => {
  test.setTimeout(300_000);
  const before = await jobIds();
  const { job } = await startScrub(page, before, 8 * MiB);
  // No browser reconnects after the reboot, so only the boot can start the host.
  await page.close();

  await rig(["reboot"]);
  // It exits after a minute idle, so this asks whether it started, not whether it still runs.
  expect(
    Number(
      await vmSsh(
        "systemctl show -P ActiveEnterTimestampMonotonic linuxio-jobs@0.service",
      ),
    ),
  ).toBeGreaterThan(0);
  // Phase 3: a scrub is resumable. The shutdown marked it interrupted/shutdown
  // with each device's offset, and the host the boot started resumed it.
  const done = await waitForJob(SCRUB, before, "completed", {
    timeout: 180_000,
  });
  expect(done.id).toBe(job.id);
  expect(done.checkpoint).toMatchObject({ remaining: expect.any(Object) });
  expect(
    Object.keys((done.checkpoint as { remaining: object }).remaining),
  ).not.toHaveLength(0);
  expect((await loggedJobs("job resumed")).map((j) => j.id)).toContain(job.id);
});

test("after a power loss the root job host marks the job interrupted/reboot", async ({
  page,
}) => {
  test.setTimeout(300_000);
  // A copy is not resumable, so the record stays interrupted/reboot. The job
  // host's reads are capped at 20 MB/s, so the 2 GiB copy takes about 100 s;
  // the cap lives in /run and goes with the power.
  const dir = `${HOME}/e2e-power`;
  await vmSsh(
    `sudo rm -rf ${dir} && mkdir -p ${dir}/dst && ` +
      `dd if=/dev/zero of=${dir}/big.bin bs=1M count=2048 status=none && sync && ` +
      `echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null && ` +
      `sudo systemctl set-property --runtime linuxio-jobs@0.service 'IOReadBandwidthMax=/dev/vda 20M'`,
  );
  try {
    const before = await jobIds();
    await signIn(page);
    await page.goto(`/filebrowser${dir}`);
    await page.getByText("big.bin", { exact: true }).click();
    await page.keyboard.press("Control+c");
    await page.getByText("dst", { exact: true }).dblclick();
    await expect(page.getByText("This folder is empty.")).toBeVisible();
    await page.keyboard.press("Control+v");
    await waitForJob("filebrowser.copy_batch", before, "running");
    await page.close();
    // A job's first record write is not fsynced (jobs.md 2.10).
    await vmSsh("sync");
    expect(
      (await jobRecords("filebrowser.copy_batch")).filter(
        (r) => !before.has(r.id) && r.state === "running",
      ),
      "the copy ended before the power cut: raise the file size",
    ).toHaveLength(1);

    await rig(["power-cut"]);
    const job = await waitForJob(
      "filebrowser.copy_batch",
      before,
      "interrupted",
      {
        reason: "reboot",
      },
    );
    expect(await loggedJobs("job marked interrupted")).toContainEqual({
      id: job.id,
      reason: "reboot",
    });
    expect((await loggedJobs("job resumed")).map((j) => j.id)).not.toContain(
      job.id,
    );
  } finally {
    await vmSsh(
      `sudo systemctl set-property --runtime linuxio-jobs@0.service IOReadBandwidthMax= && sudo rm -rf ${dir}`,
    );
  }
});
