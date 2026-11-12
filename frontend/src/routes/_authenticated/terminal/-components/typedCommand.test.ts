import { describe, expect, it } from "vitest";

import { typedCommandFromState, typedCommandInput } from "./typedCommand";

describe("typedCommandFromState", () => {
  it("keeps a plain command and drops the newline that would run it", () => {
    expect(typedCommandFromState("sudo virsh list --all\n")).toBe(
      "sudo virsh list --all",
    );
  });

  it("drops control characters an agent could use to press keys", () => {
    // Ctrl+C, an escape that would end a bracketed paste early, and a carriage
    // return that would run the line.
    expect(typedCommandFromState("ls\u0003 \u001b[201~-la\r")).toBe(
      "ls [201~-la",
    );
  });

  it("turns a tab into spaces so the shell does not start completion", () => {
    expect(typedCommandFromState("echo\tdone")).toBe("echo    done");
  });

  it("keeps the lines of a command with several", () => {
    expect(typedCommandFromState("sudo docker ps -a\r\nsudo virsh list")).toBe(
      "sudo docker ps -a\nsudo virsh list",
    );
  });

  it("refuses anything that is not a non-empty, reasonably short string", () => {
    expect(typedCommandFromState(undefined)).toBeNull();
    expect(typedCommandFromState(["ls"])).toBeNull();
    expect(typedCommandFromState(" \n\n")).toBeNull();
    expect(typedCommandFromState("x".repeat(8001))).toBeNull();
  });
});

describe("typedCommandInput", () => {
  it("types a one-line command as it is, with no Enter", () => {
    expect(typedCommandInput("uptime", false)).toBe("uptime");
  });

  it("sends several lines as one bracketed paste, which a shell does not run", () => {
    expect(typedCommandInput("one\ntwo", true)).toBe(
      "\u001b[200~one\rtwo\u001b[201~",
    );
  });

  it("holds several lines back while the shell would run each one", () => {
    expect(typedCommandInput("one\ntwo", false)).toBeNull();
  });
});
