import { useSuspenseQuery } from "@tanstack/react-query";
import { Suspense } from "react";
import { describe, expect, it, vi } from "vitest";

import { createTestQueryClient, render, screen, waitFor } from "@/test/render";

import ErrorBoundary from "./ErrorBoundary";

describe("ErrorBoundary", () => {
  it("resets a failed suspense query before retrying the widget", async () => {
    const query = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error("first request failed"))
      .mockResolvedValue("Recovered data");

    function SuspenseWidget() {
      const { data } = useSuspenseQuery({
        queryFn: query,
        queryKey: ["error-boundary-retry"],
        retry: false,
      });
      return <div>{data}</div>;
    }

    const queryClient = createTestQueryClient();
    const { user } = render(
      <Suspense fallback={<div>Loading widget</div>}>
        <ErrorBoundary>
          <SuspenseWidget />
        </ErrorBoundary>
      </Suspense>,
      { queryClient },
    );

    expect(
      await screen.findByText("Something went wrong in this widget."),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Retry widget" }));

    expect(await screen.findByText("Recovered data")).toBeInTheDocument();
    await waitFor(() => expect(query).toHaveBeenCalledTimes(2));
  });
});
