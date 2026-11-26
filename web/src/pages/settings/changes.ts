import type { AppName } from "@/api/types";

export type ConnectionEdits = Record<AppName, Record<string, string | null | undefined>>;
export type ConnectionChanges = Partial<Record<AppName, Record<string, string | null>>>;

// connectionChanges returns only the connection fields the user changed, shaped as the
// request's `connections` object. undefined means untouched and an empty string means an
// input left empty: neither is sent, because the server clears a saved value for "" (spec
// 9.2). null is the explicit "clear saved value". No change gives undefined.
export function connectionChanges(edits: ConnectionEdits): ConnectionChanges | undefined {
  const out: ConnectionChanges = {};
  for (const app of Object.keys(edits) as AppName[]) {
    const fields: Record<string, string | null> = {};
    for (const [name, v] of Object.entries(edits[app])) {
      if (v === undefined || v === "") continue;
      fields[name] = v;
    }
    if (Object.keys(fields).length > 0) out[app] = fields;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}
