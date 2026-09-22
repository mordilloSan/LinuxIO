import { expect, test } from "@playwright/test";

test("opens retained journal history for the selected unit and invocation", async ({
  page,
}) => {
  const requests: Array<Record<string, unknown>> = [];
  await page.routeWebSocket("**/ws", (socket) => {
    socket.onMessage((message) => {
      if (typeof message === "string" || message[4] !== 0x01) return;
      const { route, request } = JSON.parse(message.subarray(14).toString());
      if (route === "logs.general.follow") requests.push(request);
    });
  });
  const unit = "linuxio-schedule-123e4567-e89b-12d3-a456-426614174000.service";
  const invocationId = "0123456789abcdef0123456789abcdef";
  await page.goto(`/logs?unit=${unit}&invocationId=${invocationId}`);
  await expect(
    page.getByText(`${unit} · invocation ${invocationId}`, { exact: false }),
  ).toBeVisible();
  await expect.poll(() => requests.length).toBeGreaterThan(0);
  expect(requests.at(-1)).toMatchObject({ unit, invocationId, timePeriod: "" });
  await page.reload();
  await expect.poll(() => requests.length).toBeGreaterThan(1);
  expect(requests.at(-1)).toMatchObject({ unit, invocationId, timePeriod: "" });
});
