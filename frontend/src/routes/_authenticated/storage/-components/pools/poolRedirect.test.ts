import { isRedirect } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import { Route } from "../../index";

const beforeLoad = Route.options.beforeLoad as (args: {
  search: { pool?: string };
  context: { access: { privileged: boolean; btrfsAvailable?: boolean } };
}) => void;

const admin = { access: { privileged: true, btrfsAvailable: true } };

describe("/storage?pool=", () => {
  it("sends an old pool link to the pool's page", () => {
    let thrown: unknown;
    try {
      beforeLoad({ search: { pool: "uuid-raid" }, context: admin });
    } catch (error) {
      thrown = error;
    }
    expect(isRedirect(thrown)).toBe(true);
    expect((thrown as { options: unknown }).options).toMatchObject({
      to: "/storage/pools/$uuid",
      params: { uuid: "uuid-raid" },
    });
  });

  it("leaves the Disks overview alone without one", () => {
    expect(() => beforeLoad({ search: {}, context: admin })).not.toThrow();
  });

  it("leaves a session that cannot open pools on the Disks overview", () => {
    expect(() =>
      beforeLoad({
        search: { pool: "uuid-raid" },
        context: { access: { privileged: false, btrfsAvailable: true } },
      }),
    ).not.toThrow();
  });
});
