import { describe, expect, it } from "vitest";

import type { AccountGroup, AccountUser, SharedFolderAccess } from "@/api";

import {
  accessCandidates,
  accessLowered,
  accessProblem,
  accessSummary,
  defaultAccess,
  networkAddresses,
  timeMachineName,
} from "./sharedFolders";

const row = (
  kind: "user" | "group",
  name: string,
  level: "rw" | "ro" | "none",
): SharedFolderAccess => ({ kind, name, level });

const user = (username: string, uid: number) =>
  ({ username, uid }) as AccountUser;
const group = (name: string, gid: number, members: string[] = []) =>
  ({ name, gid, members }) as AccountGroup;

describe("accessSummary", () => {
  it("counts the people and the groups with access", () => {
    expect(
      accessSummary({
        access: [
          row("user", "alice", "rw"),
          row("user", "bob", "ro"),
          row("group", "family", "ro"),
          row("group", "kids", "none"),
        ],
        everyone: "none",
      }),
    ).toBe("3 people and groups have access");
    expect(
      accessSummary({ access: [row("user", "alice", "rw")], everyone: "none" }),
    ).toBe("1 person has access");
    expect(
      accessSummary({
        access: [row("user", "alice", "rw"), row("user", "bob", "ro")],
        everyone: "none",
      }),
    ).toBe("2 people have access");
    expect(
      accessSummary({
        access: [row("group", "family", "ro")],
        everyone: "none",
      }),
    ).toBe("1 group has access");
  });

  it("says when nobody, or everyone, has access", () => {
    expect(
      accessSummary({ access: [row("user", "bob", "none")], everyone: "none" }),
    ).toBe("Nobody has access");
    expect(accessSummary({ access: [], everyone: "ro" })).toBe(
      "Everyone who signs in can read",
    );
    expect(
      accessSummary({ access: [row("user", "bob", "none")], everyone: "rw" }),
    ).toBe("Everyone who signs in can read and write");
  });
});

describe("accessCandidates", () => {
  it("offers the regular users and groups not in the table yet", () => {
    const candidates = accessCandidates(
      [
        user("root", 0),
        user("alice", 1000),
        user("bob", 1001),
        user("nobody", 65534),
      ],
      [
        group("docker", 998),
        group("alice", 1000),
        group("family", 2000),
        group("nogroup", 65534),
      ],
      [row("user", "alice", "rw"), row("group", "family", "ro")],
    );
    expect(candidates).toEqual([
      { kind: "user", name: "bob" },
      { kind: "group", name: "alice" },
    ]);
  });
});

describe("defaultAccess", () => {
  it("starts with the administrator who is making the folder", () => {
    expect(defaultAccess("alice", [user("alice", 1000)])).toEqual({
      access: [row("user", "alice", "rw")],
      everyone: "none",
    });
  });

  // Nobody until someone is chosen: Create then asks for a row.
  it("starts with nobody when that account cannot be given a row", () => {
    expect(defaultAccess("root", [user("root", 0)])).toEqual({
      access: [],
      everyone: "none",
    });
  });

  it("starts with nobody while the accounts are not known", () => {
    expect(defaultAccess("alice", [])).toEqual({
      access: [],
      everyone: "none",
    });
  });
});

describe("accessProblem", () => {
  it("refuses SMB with nobody let in", () => {
    expect(accessProblem([row("user", "bob", "none")], "none", true)).toBe(
      "Give someone access before sharing over SMB",
    );
    expect(
      accessProblem([row("user", "bob", "none")], "none", false),
    ).toBeNull();
    expect(accessProblem([row("user", "bob", "ro")], "none", true)).toBeNull();
  });
});

// Lowered to read only, or to everyone else's lower level, a person keeps
// what they had on what is already inside, locally and over NFS; no access
// holds at once, at the folder itself.
describe("accessLowered", () => {
  const before = {
    access: [row("user", "alice", "rw"), row("group", "family", "ro")],
    everyone: "ro" as const,
  };

  it("says so for read and write lowered to read only", () => {
    expect(
      accessLowered(before, {
        ...before,
        access: [row("user", "alice", "ro"), row("group", "family", "ro")],
      }),
    ).toBe(true);
  });

  // Who loses a read-and-write row may keep read only through a group,
  // which the table alone does not say.
  it("says so for a read-and-write row removed", () => {
    expect(
      accessLowered(before, {
        access: [row("group", "family", "ro")],
        everyone: "none",
      }),
    ).toBe(true);
  });

  it("says nothing for a read-only row removed", () => {
    expect(
      accessLowered(before, {
        access: [row("user", "alice", "rw")],
        everyone: "none",
      }),
    ).toBe(false);
  });

  it("says nothing for a read-and-write row removed when everyone else may write", () => {
    expect(
      accessLowered(before, {
        access: [row("group", "family", "ro")],
        everyone: "rw",
      }),
    ).toBe(false);
  });

  it("says so for a row removed while everyone else can still read", () => {
    expect(
      accessLowered(before, {
        ...before,
        access: [row("group", "family", "ro")],
      }),
    ).toBe(true);
  });

  it("says so for everyone else lowered from read and write", () => {
    expect(
      accessLowered(
        { access: [], everyone: "rw" },
        { access: [], everyone: "ro" },
      ),
    ).toBe(true);
  });

  it("says nothing for no access, which holds at once", () => {
    expect(
      accessLowered(before, {
        access: [row("user", "alice", "none"), row("group", "family", "none")],
        everyone: "none",
      }),
    ).toBe(false);
  });

  it("says nothing when access only grows", () => {
    expect(
      accessLowered(before, {
        access: [row("user", "alice", "rw"), row("group", "family", "rw")],
        everyone: "rw",
      }),
    ).toBe(false);
  });
});

// smbd tells Macs of its Time Machine folders when it starts, so a change of
// the name one is announced under restarts it.
describe("timeMachineName", () => {
  it("is the share name of a Time Machine folder shared over SMB", () => {
    expect(
      timeMachineName({ name: "Backups", smb: true, timeMachine: true }),
    ).toBe("Backups");
    expect(
      timeMachineName({ name: "Backups", smb: false, timeMachine: true }),
    ).toBe("");
    expect(
      timeMachineName({ name: "Backups", smb: true, timeMachine: false }),
    ).toBe("");
  });
});

describe("networkAddresses", () => {
  it("gives the address for Windows and for a Mac", () => {
    expect(networkAddresses("nas.lan", "Media")).toBe(
      "\\\\nas.lan\\Media on Windows, smb://nas.lan/Media on a Mac",
    );
  });
});
