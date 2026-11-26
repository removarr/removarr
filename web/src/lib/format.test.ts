import { describe, expect, it } from "vitest";
import { formatDate, formatDays, formatRatio, formatSize } from "./format";

describe("format", () => {
  it("formats sizes in IEC units with one decimal", () => {
    expect(formatSize(0)).toBe("0 B");
    expect(formatSize(1023)).toBe("1023 B");
    expect(formatSize(1536)).toBe("1.5 KiB");
    expect(formatSize(52428800)).toBe("50.0 MiB");
    expect(formatSize(5 * 1024 ** 3)).toBe("5.0 GiB");
    expect(formatSize(2.5 * 1024 ** 4)).toBe("2.5 TiB");
  });
  it("says unknown for a missing date", () => {
    expect(formatDate(undefined)).toBe("unknown");
    expect(formatDate(null)).toBe("unknown");
  });
  it("shows a dash for a missing ratio or seeding time", () => {
    expect(formatRatio(null)).toBe("–");
    expect(formatRatio(1.234)).toBe("1.23");
    expect(formatDays(null)).toBe("–");
    expect(formatDays(12.6)).toBe("12.6 d");
  });
});
