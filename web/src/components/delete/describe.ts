import type { KeptTorrent, ManagerStep, Plan, PlanRow, PlanTorrent, RowRef } from "@/api/types";

// The words for a plan, and the plan grouped by the app each part changes (spec 7.4).

const EXCLUSION = ", and add it to the import exclusion list";

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

// describeStep says in words what a media manager step does to the row titled rowTitle.
export function describeStep(step: ManagerStep, rowTitle: string): string {
  const exclusion = step.addExclusion ? EXCLUSION : "";
  switch (step.action) {
    case "remove-movie":
      return `Remove ${rowTitle} from Radarr with its files${exclusion}`;
    case "remove-series":
      return `Remove ${rowTitle} from Sonarr with its files, specials included${exclusion}`;
    case "unmonitor-episodes":
      return `Unmonitor ${count(step.episodeIds?.length ?? 0, "episode", "episodes")} of ${rowTitle}`;
    case "unmonitor-seasons": {
      const seasons = step.seasons ?? [];
      return `Unmonitor ${seasons.length === 1 ? "season" : "seasons"} ${seasons.join(", ")} of ${rowTitle}`;
    }
    case "delete-episode-files":
      return `Delete ${count(step.fileIds?.length ?? 0, "episode file", "episode files")} of ${rowTitle}`;
  }
}

// refLabel names a row reference the way rowLabel names a row: a season row adds its season.
export function refLabel(ref: RowRef): string {
  return ref.season === undefined ? ref.title : `${ref.title}, season ${ref.season}`;
}

export interface AppGroup {
  rows: PlanRow[];
  steps: { step: ManagerStep; title: string }[];
  torrents: PlanTorrent[];
  kept: KeptTorrent[];
}

const emptyGroup = (): AppGroup => ({ rows: [], steps: [], torrents: [], kept: [] });

// groupByApp sorts a plan by the app it changes: movie rows and Radarr steps under Radarr,
// series rows and Sonarr steps under Sonarr, torrent rows and every removed or kept torrent
// under qBittorrent. A step is titled by the row of its item that it acts on.
export function groupByApp(plan: Plan): { radarr: AppGroup; sonarr: AppGroup; qbittorrent: AppGroup } {
  const g = { radarr: emptyGroup(), sonarr: emptyGroup(), qbittorrent: emptyGroup() };
  const byList = { movies: g.radarr, series: g.sonarr, torrents: g.qbittorrent };
  for (const item of plan.items ?? []) {
    for (const r of item.rows) byList[r.row.list].rows.push(r);
    for (const step of item.steps ?? []) {
      const title = item.rows.find((r) => r.row.key === step.rowKey)?.row.title ?? step.rowKey;
      (step.manager === "movies" ? g.radarr : g.sonarr).steps.push({ step, title });
    }
    g.qbittorrent.torrents.push(...(item.torrents ?? []));
    // A torrent that stays can be associated with the rows of several items: list it once.
    for (const k of item.kept ?? []) {
      if (!g.qbittorrent.kept.some((x) => x.torrent.id === k.torrent.id)) g.qbittorrent.kept.push(k);
    }
  }
  return g;
}
