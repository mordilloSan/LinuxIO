import { expect, test } from "@playwright/test";

// Playwright hides native scrollbars by default; this spec needs the real thumb.
test.use({ launchOptions: { ignoreDefaultArgs: ["--hide-scrollbars"] } });

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  // Exercise the real journal page and stream lifecycle with a large backlog.
  // Outer mux id/flags: 5 bytes; bridge opcode/id/length: 9 bytes.
  await page.routeWebSocket("**/ws", (socket) => {
    socket.onMessage((message) => {
      if (typeof message === "string" || message[4] !== 0x01) return;
      const id = message.readUInt32BE(0);
      const { route, request } = JSON.parse(message.subarray(14).toString());
      if (route !== "logs.general.follow") return;
      const send = (opcode: number, payload: string) => {
        const body = Buffer.from(payload);
        const frame = Buffer.alloc(14 + body.length);
        frame.writeUInt32BE(id, 0);
        frame[4] = 0x04;
        frame[5] = opcode;
        frame.writeUInt32BE(id, 6);
        frame.writeUInt32BE(body.length, 10);
        body.copy(frame, 14);
        socket.send(frame);
      };
      const count = request.afterCursor ? 0 : 5000;
      send(
        0x81,
        Array.from({ length: count }, (_, index) =>
          JSON.stringify({
            __CURSOR: `scroll-${index}`,
            __REALTIME_TIMESTAMP: String(1788696000000000 + index * 1000000),
            PRIORITY: String(index % 8),
            SYSLOG_IDENTIFIER: "linuxio-monitoring",
            _SYSTEMD_UNIT: "linuxio-monitoring.service",
            MESSAGE: `Journal entry ${index}: monitoring system activity and network traffic`,
          }),
        ).join("\n"),
      );
      send(0x84, JSON.stringify({ type: "backlog_complete", count }));
    });
  });
});

test("keeps journal rows visible through repeated long scrollbar jumps", async ({
  page,
}, testInfo) => {
  await page.goto("/logs");
  await expect(page.getByText(/^1000 of 5000 shown/)).toBeVisible();
  const scrollport = page
    .getByRole("table", { name: "General logs" })
    .locator(".app-dt__scroll");
  for (const count of [2000, 3000, 4000, 5000]) {
    await scrollport.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect(
      page.getByText(
        new RegExp(count < 5000 ? `^${count} of 5000 shown` : "^5000 shown"),
      ),
    ).toBeVisible();
  }
  await scrollport.evaluate((element) => {
    element.scrollTop = 0;
  });
  await expect(
    page.getByText("Journal entry 4999:", { exact: false }),
  ).toBeVisible();
  await expect(scrollport.locator("[data-fast-scrolling]")).toHaveCount(0);
  // The first large jump must defer tooltip work in the scroll update itself,
  // without mounting tooltip triggers and replacing them next frame.
  const immediateDeferredCells = await scrollport.evaluate(
    (element) =>
      new Promise<number>((resolve) => {
        element.addEventListener(
          "scroll",
          () => {
            resolve(element.querySelectorAll("[data-fast-scrolling]").length);
          },
          { once: true },
        );
        element.scrollTop =
          (element.scrollHeight - element.clientHeight) * 0.75;
      }),
  );
  expect(immediateDeferredCells).toBeGreaterThan(0);
  await scrollport.evaluate((element) => {
    element.scrollTop = 0;
  });
  await expect(scrollport.locator("[data-fast-scrolling]")).toHaveCount(0);
  const cdp = await page.context().newCDPSession(page);
  // Timings are diagnostic, not machine-dependent pass/fail thresholds.
  await cdp.send("Emulation.setCPUThrottlingRate", { rate: 4 });
  await cdp.send("Performance.enable");
  const before = await cdp.send("Performance.getMetrics");
  const samples = await scrollport.evaluate(async (element) => {
    const viewport = element.getBoundingClientRect();
    const results: Array<{
      elapsed: number;
      visible: number;
      mounted: number;
      deferredCells: number;
    }> = [];
    for (let step = 0; step < 60; step++) {
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => resolve()),
      );
      const start = performance.now();
      // Jump well outside the overscan in both directions on every step.
      element.scrollTop =
        (element.scrollHeight - element.clientHeight) *
        (((step * 37) % 97) / 100);
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => resolve()),
      );
      const rows = [...element.querySelectorAll(".app-dt__virtual-row")];
      results.push({
        elapsed: performance.now() - start,
        mounted: rows.length,
        deferredCells: element.querySelectorAll("[data-fast-scrolling]").length,
        visible: rows.filter((node) => {
          const row = node.getBoundingClientRect();
          return row.bottom > viewport.top && row.top < viewport.bottom;
        }).length,
      });
    }
    return results;
  });
  const after = await cdp.send("Performance.getMetrics");
  const metrics = Object.fromEntries(
    after.metrics
      .filter(({ name }) =>
        [
          "ScriptDuration",
          "LayoutDuration",
          "RecalcStyleDuration",
          "TaskDuration",
        ].includes(name),
      )
      .map(({ name, value }) => [
        name,
        value -
          (before.metrics.find((metric) => metric.name === name)?.value ?? 0),
      ]),
  );
  const sorted = samples.map((sample) => sample.elapsed).sort((a, b) => a - b);
  console.log(
    JSON.stringify({
      metrics,
      median: sorted[30],
      p95: sorted[57],
      blanks: samples.filter((sample) => sample.visible === 0).length,
      maxMounted: Math.max(...samples.map((sample) => sample.mounted)),
    }),
  );
  await testInfo.attach("scroll-samples", {
    body: JSON.stringify({ samples, metrics }),
    contentType: "application/json",
  });
  expect(samples.every((sample) => sample.visible > 0)).toBe(true);
  expect(Math.max(...samples.map((sample) => sample.mounted))).toBeLessThan(60);
  expect(samples.slice(1).every((sample) => sample.deferredCells > 0)).toBe(
    true,
  );
  await expect(scrollport.locator("[data-fast-scrolling]")).toHaveCount(0);
  await expect(
    scrollport.getByRole("button", { name: "Expand row" }).first(),
  ).toBeVisible();

  // Also exercise Chromium's native scrollbar thumb, not just scrollTop writes.
  await scrollport.evaluate((element) => {
    element.scrollTop = 0;
  });
  await expect(scrollport.locator("[data-fast-scrolling]")).toHaveCount(0);
  const box = (await scrollport.boundingBox())!;
  expect(
    await scrollport.evaluate(
      (element) => (element as HTMLElement).offsetWidth - element.clientWidth,
    ),
  ).toBeGreaterThan(0);
  const thumbX = box.x + box.width - 4;
  await page.mouse.move(thumbX, box.y + 8);
  await page.mouse.down();
  for (const ratio of [0.85, 0.2, 0.7]) {
    await page.mouse.move(thumbX, box.y + box.height * ratio, { steps: 10 });
  }
  const dragged = await scrollport.evaluate((element) => ({
    top: element.scrollTop,
    deferredCells: element.querySelectorAll("[data-fast-scrolling]").length,
  }));
  await page.mouse.up();
  expect(dragged.top).toBeGreaterThan(100_000);
  expect(dragged.deferredCells).toBeGreaterThan(0);
  await expect(scrollport.locator("[data-fast-scrolling]")).toHaveCount(0);

  // After idle, a normal wheel step must not inherit the previous fast mode.
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, 48);
  await scrollport.evaluate(
    () =>
      new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
  );
  await expect(scrollport.locator("[data-fast-scrolling]")).toHaveCount(0);
});

for (const scheme of ["dark", "light"] as const) {
  test(`preserves log level badges and text styling during fast scrolling in ${scheme}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 900, height: 1000 });
    await page.goto(scheme === "light" ? "/styling/light/logs" : "/logs");
    await expect(page.getByText(/^1000 of 5000 shown/)).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    const scrollport = page
      .getByRole("table", { name: "General logs" })
      .locator(".app-dt__scroll");
    const snapshot = (jump: boolean) =>
      scrollport.evaluate(async (element, shouldJump) => {
        if (shouldJump) {
          await new Promise<void>((resolve) => {
            element.addEventListener("scroll", () => resolve(), { once: true });
            element.scrollTop =
              (element.scrollHeight - element.clientHeight) * 0.5;
          });
        }
        const rows = [...element.querySelectorAll(".app-dt__row--body")].slice(
          0,
          8,
        );
        let tooltips = 0;
        let truncatedMessages = 0;
        const styles = rows.map((row) => {
          const cells = row.querySelectorAll(".app-dt__cell");
          return [
            cells[1].textContent,
            [1, 3, 4].map((index) => {
              const cell = cells[index];
              const content = cell.querySelector<HTMLElement>(
                index === 1 ? ".app-chip" : ".app-typo",
              )!;
              tooltips += cell.querySelectorAll(".app-tooltip-trigger").length;
              if (index === 4 && content.scrollWidth > content.clientWidth)
                truncatedMessages++;
              const css = getComputedStyle(content);
              const box = content.getBoundingClientRect();
              const cellBox = cell.getBoundingClientRect();
              return {
                color: css.color,
                background: css.backgroundColor,
                border: css.border,
                borderRadius: css.borderRadius,
                font: css.font,
                lineHeight: css.lineHeight,
                whiteSpace: css.whiteSpace,
                textOverflow: css.textOverflow,
                x: box.x - cellBox.x,
                y: box.y - cellBox.y,
                width: box.width,
                height: box.height,
              };
            }),
          ];
        });
        return {
          styles,
          tooltips,
          truncatedMessages,
          fast: element.querySelectorAll("[data-fast-scrolling]").length,
        };
      }, jump);

    const fast = await snapshot(true);
    expect(fast.fast).toBeGreaterThan(0);
    expect(fast.tooltips).toBe(0);
    expect(fast.truncatedMessages).toBeGreaterThan(0);
    await expect(scrollport.locator("[data-fast-scrolling]")).toHaveCount(0);
    const idle = await snapshot(false);
    expect(idle.tooltips).toBe(24);
    expect(fast.styles).toEqual(idle.styles);
    expect(new Set(idle.styles.map(([level]) => level)).size).toBe(8);
  });
}
