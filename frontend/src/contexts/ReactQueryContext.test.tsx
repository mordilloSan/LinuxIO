import { useQueryClient } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { useEffect } from "react";
import { describe, expect, it } from "vitest";

import ReactQueryProvider, {
  createQueryClient,
  getAppQueryClient,
} from "@/contexts/ReactQueryContext";

describe("ReactQueryProvider", () => {
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
      <ReactQueryProvider>
        <Observer />
      </ReactQueryProvider>,
    );

    await Promise.resolve();
    expect(observedClient).toBe(getAppQueryClient());
    expect(getAppQueryClient()).toBe(observedClient);
  });

  it("keeps explicitly created clients isolated for tests and server renders", () => {
    expect(createQueryClient()).not.toBe(createQueryClient());
  });
});
