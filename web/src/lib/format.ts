const UNITS = ["KiB", "MiB", "GiB", "TiB"];

// formatSize shows a byte count in IEC units with one decimal (spec 6.9 calls it "size").
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  let value = bytes / 1024;
  let unit = 0;
  // Step up while the value would print as 1024 or more, so 1023.96 KiB reads 1.0 MiB.
  while (unit < UNITS.length - 1 && Number(value.toFixed(1)) >= 1024) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(1)} ${UNITS[unit]}`;
}

const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });
const dateTimeFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function parse(iso?: string | null): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

// formatDate shows a date in the browser's locale; an absent date is "unknown" (spec 6.1).
export function formatDate(iso?: string | null): string {
  const d = parse(iso);
  return d ? dateFormat.format(d) : "unknown";
}

// formatDateTime shows a date and time in the browser's locale.
export function formatDateTime(iso?: string | null): string {
  const d = parse(iso);
  return d ? dateTimeFormat.format(d) : "unknown";
}

// formatRatio shows a torrent's share ratio; the download client may not report one.
export function formatRatio(r: number | null): string {
  return r === null ? "–" : r.toFixed(2);
}

// formatDays shows a seeding time in days; the download client may not report one.
export function formatDays(d: number | null): string {
  return d === null ? "–" : `${d.toFixed(1)} d`;
}
