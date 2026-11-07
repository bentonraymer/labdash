import { describe, expect, it } from "vitest";
import { diffLines } from "./diff";

describe("diffLines", () => {
  it("finds added and removed lines", () => {
    const d = diffLines("a\nb\nc", "a\nB\nc\nd");
    expect(d).toEqual([
      { kind: "same", text: "a" },
      { kind: "del", text: "b" },
      { kind: "add", text: "B" },
      { kind: "same", text: "c" },
      { kind: "add", text: "d" },
    ]);
  });
  it("handles identical and empty inputs", () => {
    expect(diffLines("x", "x")).toEqual([{ kind: "same", text: "x" }]);
    expect(diffLines("", "y").filter((l) => l.kind !== "same")).toEqual([
      { kind: "del", text: "" },
      { kind: "add", text: "y" },
    ]);
  });
});
