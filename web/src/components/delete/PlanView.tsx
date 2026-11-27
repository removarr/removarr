import { useId, type ReactNode } from "react";
import type { KeptTorrent, Plan, PlanItem, PlanRow, PlanTorrent } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { formatDateTime, formatSize } from "@/lib/format";
import { rowLabel } from "@/lib/selection";
import { describeStep, groupByApp, type AppGroup } from "./describe";

const KEPT_REASON: Record<KeptTorrent["reason"], string> = {
  queued: "In a download queue",
  "backs-library": "Still backs a library file that stays",
};

// Group is one app's part of the plan, a labelled region so it can be found by its name.
function Group({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="flex flex-col gap-2">
      <h3 id={id} className="text-sm font-semibold">{title}</h3>
      {children}
    </section>
  );
}

// sharedThrough names what an added row shares with the selected row: the torrents of its
// item whose IDs it lists, or a shared library file when none of them is a torrent.
function sharedThrough(r: PlanRow, item: PlanItem): string {
  const torrents = [...(item.torrents ?? []).map((t) => t.torrent), ...(item.kept ?? []).map((k) => k.torrent)];
  const names = (r.shares ?? []).flatMap((id) => torrents.find((t) => t.id === id)?.name ?? []);
  return names.length > 0 ? names.join(", ") : "a shared library file";
}

function RowList({ group, plan }: { group: AppGroup; plan: Plan }) {
  const itemOf = (r: PlanRow) => (plan.items ?? []).find((i) => i.rows.includes(r))!;
  return (
    <>
      <ul className="flex flex-col gap-1 text-sm">
        {group.rows.map((r) => (
          <li key={`${r.row.list}:${r.row.key}`} className="flex flex-col gap-0.5">
            <div className="flex items-baseline justify-between gap-2">
              <span className="font-medium">{rowLabel(r.row)}</span>
              <span className="text-muted-foreground tabular-nums">{formatSize(r.row.size)}</span>
            </div>
            {(r.added || r.notInScan) && (
              <div className="flex flex-wrap gap-1">
                {r.added && r.addedWith && (
                  <span className="text-xs text-muted-foreground">
                    {`Added: deleted together with ${rowLabel(r.addedWith)}, through ${sharedThrough(r, itemOf(r))}`}
                  </span>
                )}
                {r.notInScan && <Badge variant="outline">Not in the latest scan</Badge>}
              </div>
            )}
          </li>
        ))}
      </ul>
      {group.steps.length > 0 && (
        <ol className="flex list-decimal flex-col gap-0.5 pl-5 text-sm text-muted-foreground">
          {group.steps.map((s, i) => (
            <li key={i}>{describeStep(s.step, s.title)}</li>
          ))}
        </ol>
      )}
    </>
  );
}

function Torrent({ t }: { t: PlanTorrent }) {
  return (
    <li className="flex flex-col gap-0.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="font-medium break-all">{t.torrent.name}</span>
        <span className="text-muted-foreground tabular-nums">{formatSize(t.torrent.size)}</span>
      </div>
      <div className="flex flex-wrap gap-1">
        {t.torrent.belowSeedMin && <Badge variant="secondary">Below seeding minimum</Badge>}
        {t.torrent.outsideLibrary !== undefined && (
          <Badge variant="outline">Outside the library: {formatSize(t.torrent.outsideLibrary)}</Badge>
        )}
        {t.keepData && <Badge variant="outline">Keeps its data: shared with another download, or its path is unknown</Badge>}
        {t.linkedBySize && <Badge variant="destructive">Linked by file size only: check that it belongs</Badge>}
      </div>
    </li>
  );
}

const hasRows = (g: AppGroup) => g.rows.length > 0 || g.steps.length > 0;

// PlanView shows a plan for confirmation, grouped by app (spec 7.4): what each app is asked
// to do, the torrents that go and those that stay with the reason, and the skipped rows.
export function PlanView({ plan, dryRun }: { plan: Plan; dryRun: boolean }) {
  const g = groupByApp(plan);
  const items = plan.items ?? [];
  const size = items.reduce((sum, i) => sum + i.rows.reduce((s, r) => s + r.row.size, 0), 0);
  const skipped = plan.skipped ?? [];
  return (
    <div className="flex flex-col gap-4">
      {dryRun && (
        <p role="note" className="rounded-md border border-amber-500/50 bg-amber-500/10 px-3 py-2 text-sm">
          Dry run: nothing will be changed. The history records what would have run.
        </p>
      )}
      {items.length === 0 ? (
        <p className="text-sm">Nothing can be deleted.</p>
      ) : (
        <p className="text-sm font-medium tabular-nums">
          {`${items.length} ${items.length === 1 ? "item" : "items"} · ${formatSize(size)}`}
        </p>
      )}
      {hasRows(g.radarr) && (
        <Group title="Radarr">
          <RowList group={g.radarr} plan={plan} />
        </Group>
      )}
      {hasRows(g.sonarr) && (
        <Group title="Sonarr">
          <RowList group={g.sonarr} plan={plan} />
        </Group>
      )}
      {(g.qbittorrent.torrents.length > 0 || g.qbittorrent.kept.length > 0) && (
        <Group title="qBittorrent">
          {g.qbittorrent.torrents.length > 0 && (
            <>
              <h4 className="text-xs font-medium text-muted-foreground">Removed</h4>
              <ul className="flex flex-col gap-2 text-sm">
                {g.qbittorrent.torrents.map((t) => (
                  <Torrent key={t.torrent.id} t={t} />
                ))}
              </ul>
            </>
          )}
          {g.qbittorrent.kept.length > 0 && (
            <>
              <h4 className="text-xs font-medium text-muted-foreground">Stay</h4>
              <ul className="flex flex-col gap-1 text-sm">
                {g.qbittorrent.kept.map((k) => (
                  <li key={k.torrent.id} className="flex flex-wrap items-baseline justify-between gap-2">
                    <span className="break-all">{k.torrent.name}</span>
                    <span className="text-muted-foreground">{KEPT_REASON[k.reason]}</span>
                  </li>
                ))}
              </ul>
            </>
          )}
        </Group>
      )}
      {skipped.length > 0 && (
        <Group title="Skipped">
          <ul className="flex flex-col gap-1 text-sm">
            {skipped.map((s) => (
              <li key={`${s.row.list}:${s.row.key}`}>{`${rowLabel(s.row)}: ${s.reason}`}</li>
            ))}
          </ul>
        </Group>
      )}
      <p className="text-xs text-muted-foreground">{`Valid until ${formatDateTime(plan.expiresAt)}`}</p>
    </div>
  );
}
