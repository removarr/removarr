import { useState, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import { useSaveSettings } from "@/api/queries";
import type { Rules as RulesType, Settings } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const SELECT_CLASS =
  "h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-xs dark:bg-input/30";

// Edits hold the text of the inputs as typed; changed() turns them into the fields that
// differ from the saved rules.
interface Edits {
  watchedMode?: RulesType["watchedMode"];
  seriesUnit?: RulesType["seriesUnit"];
  protectFavorites?: boolean;
  addExclusionOnDelete?: boolean;
  graceDays?: string;
  protectionTag?: string;
  minRatio?: string;
  minSeedDays?: string;
  excludedCategories?: string;
  excludedTags?: string;
}

const splitList = (s: string) => s.split(",").map((x) => x.trim()).filter((x) => x !== "");
const same = (a: string[], b: string[]) => a.length === b.length && a.every((x, i) => x === b[i]);

// changed returns only the rule fields the user changed. An empty or unreadable number is not
// a change.
function changed(rules: RulesType, e: Edits): Partial<RulesType> {
  const out: Partial<RulesType> = {};
  if (e.watchedMode !== undefined && e.watchedMode !== rules.watchedMode) out.watchedMode = e.watchedMode;
  if (e.seriesUnit !== undefined && e.seriesUnit !== rules.seriesUnit) out.seriesUnit = e.seriesUnit;
  if (e.protectFavorites !== undefined && e.protectFavorites !== rules.protectFavorites) out.protectFavorites = e.protectFavorites;
  if (e.addExclusionOnDelete !== undefined && e.addExclusionOnDelete !== rules.addExclusionOnDelete) {
    out.addExclusionOnDelete = e.addExclusionOnDelete;
  }
  if (e.protectionTag !== undefined && e.protectionTag !== rules.protectionTag) out.protectionTag = e.protectionTag;
  for (const k of ["graceDays", "minRatio", "minSeedDays"] as const) {
    const text = e[k];
    if (text === undefined || text.trim() === "") continue;
    const n = Number(text);
    if (Number.isFinite(n) && n !== rules[k]) out[k] = n;
  }
  for (const k of ["excludedCategories", "excludedTags"] as const) {
    const text = e[k];
    if (text === undefined) continue;
    const list = splitList(text);
    if (!same(list, rules[k])) out[k] = list;
  }
  return out;
}

function Field({ name, label, error, children }: { name: string; label: string; error?: string; children: ReactNode }) {
  return (
    <div data-field={`rules.${name}`} className="space-y-1.5">
      <Label htmlFor={`rule-${name}`}>{label}</Label>
      {children}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </div>
  );
}

// Rules edits the rules that decide what is suggested (spec 5.6). Only the changed fields
// are sent.
export function Rules({ settings }: { settings: Settings }) {
  const rules = settings.rules;
  const [edits, setEdits] = useState<Edits>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [general, setGeneral] = useState<string>();
  const [saved, setSaved] = useState(false);
  const save = useSaveSettings();

  const set = (patch: Edits) => { setEdits((e) => ({ ...e, ...patch })); setSaved(false); };
  const diff = changed(rules, edits);
  const any = Object.keys(diff).length > 0;
  const err = (n: string) => errors[`rules.${n}`];

  const onSave = () => {
    if (!any) return;
    setErrors({});
    setGeneral(undefined);
    save.mutate({ rules: diff }, {
      onSuccess: () => { setEdits({}); setSaved(true); },
      onError: (e) => {
        if (e instanceof ApiError && e.field) setErrors({ [e.field]: e.message });
        else setGeneral(e.message);
      },
    });
  };

  const text = (k: "graceDays" | "minRatio" | "minSeedDays" | "protectionTag" | "excludedCategories" | "excludedTags") =>
    edits[k] ?? (Array.isArray(rules[k]) ? (rules[k] as string[]).join(", ") : String(rules[k]));

  return (
    <section aria-label="Rules" className="space-y-4">
      <h2 className="text-lg font-semibold">Rules</h2>
      <div className="grid gap-4 md:grid-cols-2">
        <Field name="watchedMode" label="Watched mode" error={err("watchedMode")}>
          <select id="rule-watchedMode" className={SELECT_CLASS} value={edits.watchedMode ?? rules.watchedMode}
            onChange={(e) => set({ watchedMode: e.target.value as RulesType["watchedMode"] })}>
            <option value="all">All counting users have watched it</option>
            <option value="any">Any counting user has watched it</option>
          </select>
        </Field>
        <Field name="graceDays" label="Grace period (days)" error={err("graceDays")}>
          <Input id="rule-graceDays" type="number" value={text("graceDays")} onChange={(e) => set({ graceDays: e.target.value })} />
        </Field>
        <Field name="seriesUnit" label="Series unit" error={err("seriesUnit")}>
          <select id="rule-seriesUnit" className={SELECT_CLASS} value={edits.seriesUnit ?? rules.seriesUnit}
            onChange={(e) => set({ seriesUnit: e.target.value as RulesType["seriesUnit"] })}>
            <option value="series">Whole series</option>
            <option value="season">Per season</option>
          </select>
        </Field>
        <Field name="protectionTag" label="Protection tag" error={err("protectionTag")}>
          <Input id="rule-protectionTag" value={text("protectionTag")} onChange={(e) => set({ protectionTag: e.target.value })} />
        </Field>
        <Field name="minRatio" label="Minimum ratio (0 = off)" error={err("minRatio")}>
          <Input id="rule-minRatio" type="number" step="any" value={text("minRatio")} onChange={(e) => set({ minRatio: e.target.value })} />
        </Field>
        <Field name="minSeedDays" label="Minimum seeding time (days, 0 = off)" error={err("minSeedDays")}>
          <Input id="rule-minSeedDays" type="number" step="any" value={text("minSeedDays")} onChange={(e) => set({ minSeedDays: e.target.value })} />
        </Field>
        <Field name="excludedCategories" label="Excluded categories" error={err("excludedCategories")}>
          <Input id="rule-excludedCategories" placeholder="Comma-separated" value={text("excludedCategories")}
            onChange={(e) => set({ excludedCategories: e.target.value })} />
        </Field>
        <Field name="excludedTags" label="Excluded tags" error={err("excludedTags")}>
          <Input id="rule-excludedTags" placeholder="Comma-separated" value={text("excludedTags")}
            onChange={(e) => set({ excludedTags: e.target.value })} />
        </Field>
      </div>
      <div className="flex items-center gap-2">
        <Checkbox id="rule-protectFavorites" checked={edits.protectFavorites ?? rules.protectFavorites}
          onCheckedChange={(c) => set({ protectFavorites: c === true })} />
        <Label htmlFor="rule-protectFavorites">Protect favourites</Label>
      </div>
      <div className="flex items-center gap-2">
        <Checkbox id="rule-addExclusionOnDelete" checked={edits.addExclusionOnDelete ?? rules.addExclusionOnDelete}
          onCheckedChange={(c) => set({ addExclusionOnDelete: c === true })} />
        <Label htmlFor="rule-addExclusionOnDelete">Add to exclusion list on delete</Label>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button onClick={onSave} disabled={!any || save.isPending}>Save rules</Button>
        {saved && <p role="status" className="text-sm">Saved. Scan again to apply the changes to the lists.</p>}
        {general && <p role="alert" className="text-sm text-destructive">{general}</p>}
      </div>
    </section>
  );
}
