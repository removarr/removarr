import { describe, expect, it } from "vitest";
import { prune, rowLabel, selectAll, selectedRows, toggle, totalSize } from "./selection";
import type { Candidate } from "@/api/types";

const row = (key: string, size: number): Candidate =>
  ({ list: "movies", key, title: key, size, torrents: [], belowSeedMin: false });

describe("selection", () => {
  const a = row("a", 1), b = row("b", 2), c = row("c", 4);
  it("selects every visible row", () => {
    expect([...selectAll([a, b])]).toEqual(["a", "b"]);
  });
  it("toggles one row", () => {
    expect(toggle(new Set(["a"]), "a").has("a")).toBe(false);
    expect(toggle(new Set(), "a").has("a")).toBe(true);
  });
  it("drops rows the filter or a reload no longer shows", () => {
    expect([...prune(new Set(["a", "b", "c"]), [a, c])]).toEqual(["a", "c"]);
  });
  it("returns the selected rows in list order and their total size", () => {
    const rows = selectedRows(new Set(["c", "a"]), [a, b, c]);
    expect(rows.map((r) => r.key)).toEqual(["a", "c"]);
    expect(totalSize(rows)).toBe(5);
  });
  it("labels a row by its title, and a season row also by its season", () => {
    expect(rowLabel(a)).toBe("a");
    const season = (n: number): Candidate => ({ ...a, list: "series", scope: "season", season: n });
    expect(rowLabel(season(3))).toBe("a, season 3");
    expect(rowLabel(season(0))).toBe("a, season 0");
    expect(rowLabel({ title: "a", season: 2 })).toBe("a, season 2");
  });
});
