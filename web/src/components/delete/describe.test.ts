import { describe, expect, it } from "vitest";
import type { ManagerStep } from "@/api/types";
import { planFixture } from "@/test/fixtures";
import { describeStep, groupByApp } from "./describe";

const base = { rowKey: "k", itemId: 1 };

describe("describeStep", () => {
  it.each<[string, ManagerStep, string]>([
    ["remove-movie", { ...base, manager: "movies", action: "remove-movie" }, "Remove Alpha from Radarr with its files"],
    [
      "remove-movie with exclusion",
      { ...base, manager: "movies", action: "remove-movie", addExclusion: true },
      "Remove Alpha from Radarr with its files, and add it to the import exclusion list",
    ],
    ["remove-series", { ...base, manager: "series", action: "remove-series" }, "Remove Alpha from Sonarr with its files, specials included"],
    [
      "remove-series with exclusion",
      { ...base, manager: "series", action: "remove-series", addExclusion: true },
      "Remove Alpha from Sonarr with its files, specials included, and add it to the import exclusion list",
    ],
    ["unmonitor-episodes", { ...base, manager: "series", action: "unmonitor-episodes", episodeIds: [1, 2, 3] }, "Unmonitor 3 episodes of Alpha"],
    ["unmonitor-episodes, one", { ...base, manager: "series", action: "unmonitor-episodes", episodeIds: [1] }, "Unmonitor 1 episode of Alpha"],
    ["unmonitor-seasons, one", { ...base, manager: "series", action: "unmonitor-seasons", seasons: [2] }, "Unmonitor season 2 of Alpha"],
    ["unmonitor-seasons", { ...base, manager: "series", action: "unmonitor-seasons", seasons: [1, 2, 3] }, "Unmonitor seasons 1, 2, 3 of Alpha"],
    ["delete-episode-files", { ...base, manager: "series", action: "delete-episode-files", fileIds: [5, 6] }, "Delete 2 episode files of Alpha"],
    ["delete-episode-files, one", { ...base, manager: "series", action: "delete-episode-files", fileIds: [5] }, "Delete 1 episode file of Alpha"],
  ])("%s", (_name, step, text) => {
    expect(describeStep(step, "Alpha")).toBe(text);
  });
});

describe("groupByApp", () => {
  const g = groupByApp(planFixture);

  it("puts the movie rows and their steps under Radarr, titled by their row", () => {
    expect(g.radarr.rows.map((r) => r.row.key)).toEqual(["tmdb:1", "tmdb:3", "tmdb:4"]);
    expect(g.radarr.steps.map((s) => [s.step.action, s.title])).toEqual([
      ["remove-movie", "Alpha"],
      ["remove-movie", "Beta"],
      ["remove-movie", "Delta"],
    ]);
    expect(g.radarr.torrents).toEqual([]);
    expect(g.radarr.kept).toEqual([]);
  });

  it("puts the series rows and their steps under Sonarr", () => {
    expect(g.sonarr.rows.map((r) => r.row.key)).toEqual(["tvdb:7:2"]);
    expect(g.sonarr.steps.map((s) => [s.step.action, s.title])).toEqual([
      ["unmonitor-episodes", "Show"],
      ["unmonitor-seasons", "Show"],
      ["delete-episode-files", "Show"],
    ]);
  });

  it("puts torrent rows, removed torrents and kept torrents under qBittorrent", () => {
    expect(g.qbittorrent.rows.map((r) => r.row.key)).toEqual(["h5"]);
    expect(g.qbittorrent.steps).toEqual([]);
    expect(g.qbittorrent.torrents.map((t) => t.torrent.id)).toEqual(["h1", "h2", "h5"]);
    expect(g.qbittorrent.kept.map((k) => [k.torrent.id, k.reason])).toEqual([
      ["h3", "queued"],
      ["h4", "backs-library"],
    ]);
  });

  it("accepts a plan without items", () => {
    const empty = groupByApp({ ...planFixture, items: null });
    expect(empty.radarr.rows).toEqual([]);
    expect(empty.qbittorrent.torrents).toEqual([]);
  });
});
