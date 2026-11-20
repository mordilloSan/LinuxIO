import { expect, test, type Page } from "@playwright/test";

// Half folders, half files: `dir-00000`…, then `file-00000`…
const ENTRIES = 3000;
const LAST_FOLDER = ENTRIES / 2 - 1;
const STEPS = 60;

const name = (prefix: string, index: number) =>
  `${prefix}-${String(index).padStart(5, "0")}`;

type Sample = { frame: number; script: number };

async function openPicker(page: Page) {
  await page.goto(`/pathpicker/large-folder?entries=${ENTRIES}`);
  await expect(
    page.getByRole("heading", { name: "Path picker large folder fixture" }),
  ).toBeVisible();
  await page.getByLabel("Folder").click();
}

function summary(samples: Sample[]) {
  const at = (key: keyof Sample, share: number) => {
    const sorted = samples.map((sample) => sample[key]).sort((a, b) => a - b);
    return (
      Math.round(sorted[Math.floor((sorted.length - 1) * share)] * 10) / 10
    );
  };
  return {
    scriptMedian: at("script", 0.5),
    frameMedian: at("frame", 0.5),
    frameP95: at("frame", 0.95),
  };
}

test("opens a long folder with the current value focused and in view", async ({
  page,
}) => {
  await openPicker(page);

  const current = page.getByRole("treeitem", {
    name: name("dir", LAST_FOLDER),
  });
  await expect(current).toBeFocused();
  await expect(current).toHaveAttribute("aria-selected", "true");
  await expect(current).toBeInViewport({ ratio: 1 });
});

test("walks a long folder with the arrow keys", async ({ page }, testInfo) => {
  await openPicker(page);
  await expect(page.getByRole("treeitem")).toHaveCount(ENTRIES + 1);
  await expect(
    page.getByRole("treeitem", { name: name("dir", LAST_FOLDER) }),
  ).toBeFocused();

  // Each step waits for a frame, optionally presses a key on the focused
  // row, and times the handler (`script`) and the frame that follows.
  const walk = (key: string | null) =>
    page.evaluate(
      async ({ pressed, steps }) => {
        const frame = () =>
          new Promise<void>((resolve) =>
            requestAnimationFrame(() => resolve()),
          );
        const samples: Array<{ frame: number; script: number }> = [];
        for (let step = 0; step < steps; step++) {
          await frame();
          const start = performance.now();
          if (pressed) {
            document.activeElement?.dispatchEvent(
              new KeyboardEvent("keydown", {
                bubbles: true,
                cancelable: true,
                key: pressed,
              }),
            );
          }
          const script = performance.now() - start;
          await frame();
          samples.push({ frame: performance.now() - start, script });
        }
        return samples;
      },
      { pressed: key, steps: STEPS },
    );
  // Opens the picker five times and times each until its rows are on screen.
  const reopen = () =>
    page.evaluate(async () => {
      const frame = () =>
        new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
      const field = document.querySelector<HTMLElement>(
        'input[role="combobox"]',
      );
      const rows = () => document.querySelectorAll('[role="treeitem"]').length;
      const samples: Array<{ frame: number; script: number }> = [];
      for (let step = 0; step < 5; step++) {
        field?.click();
        while (rows() > 0) await frame();
        await frame();
        const start = performance.now();
        field?.click();
        const script = performance.now() - start;
        while (rows() <= 1) await frame();
        await frame();
        samples.push({ frame: performance.now() - start, script });
      }
      return samples;
    });
  const focusedRow = () =>
    page.evaluate(() => document.activeElement?.getAttribute("aria-label"));

  const cdp = await page.context().newCDPSession(page);
  const idle = await walk(null);
  const keys = await walk("ArrowDown");
  // Every key press moved focus one row: from the last folder into the files.
  expect(await focusedRow()).toBe(name("file", STEPS - 1));

  // The picker reopens at the current value, the last folder again.
  const opens = await reopen();
  expect(await focusedRow()).toBe(name("dir", LAST_FOLDER));

  await cdp.send("Emulation.setCPUThrottlingRate", { rate: 4 });
  const slowKeys = await walk("ArrowDown");
  await cdp.send("Emulation.setCPUThrottlingRate", { rate: 1 });
  expect(await focusedRow()).toBe(name("file", STEPS - 1));

  // Timings are diagnostic, not machine-dependent pass/fail thresholds. An
  // arrow key should cost about one idle frame: it must not draw the rows.
  const report = {
    entries: ENTRIES,
    idleFrame: summary(idle),
    arrowKey: summary(keys),
    arrowKeyCpu4x: summary(slowKeys),
    open: summary(opens),
  };
  console.log(JSON.stringify(report));
  await testInfo.attach("path-picker-timings", {
    body: JSON.stringify(report),
    contentType: "application/json",
  });
});
