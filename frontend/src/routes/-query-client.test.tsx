import { useQueryClient } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { useEffect } from "react";
import { describe, expect, it } from "vitest";

import AppQueryClientProvider, {
  createQueryClient,
  getAppQueryClient,
} from "./-query-client";

describe("AppQueryClientProvider", () => {
  it("provides the stable browser client returned to router infrastructure", async () => {
    let observedClient: ReturnType<typeof getAppQueryClient> | undefined;

    function Observer() {
      const queryClient = useQueryClient();
      useEffect(() => {
        observedClient = queryClient;
      }, [queryClient]);
      return null;
    }

    render(
      <AppQueryClientProvider>
        <Observer />
      </AppQueryClientProvider>,
    );

    await Promise.resolve();
    expect(observedClient).toBe(getAppQueryClient());
    expect(getAppQueryClient()).toBe(observedClient);
  });

  it("keeps explicitly created clients isolated for tests and server renders", () => {
    expect(createQueryClient()).not.toBe(createQueryClient());
  });
});
