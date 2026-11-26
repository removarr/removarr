import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import type { Candidate, TorrentInfo } from "@/api/types";
import { CandidateList } from "@/components/list/CandidateList";
import { renderWithProviders } from "@/test/render";
import { mediaCard, mediaColumns, mediaSortKeys } from "./MediaRow";
import { TorrentDetails } from "./TorrentDetails";
import { torrentCard, torrentColumns, torrentSortKeys } from "./TorrentRow";

const GiB = 1024 ** 3;

function torrent(over: Partial<TorrentInfo> = {}): TorrentInfo {
  return {
    id: "t1", name: "Some.Torrent.1080p", size: 8 * GiB, state: "uploading", category: "movies", tracker: "tracker.example",
    ratio: 1.5, seedingDays: 12.3, addedAt: "2026-01-01T00:00:00Z", belowSeedMin: false, ...over,
  };
}

function movie(over: Partial<Candidate> = {}): Candidate {
  return { list: "movies", key: "movie:1", title: "Alpha", year: 1999, size: 4 * GiB, torrents: [], belowSeedMin: false, ...over };
}

beforeAll(() => {
  window.matchMedia = ((q: string) => ({
    matches: true, media: q, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {},
    onchange: null, dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

function renderMedia(rows: Candidate[]) {
  return renderWithProviders(
    <CandidateList
      rows={rows}
      columns={mediaColumns}
      sortKeys={mediaSortKeys}
      renderCard={mediaCard}
      renderExpanded={(r) => <TorrentDetails row={r} />}
      actions={() => null}
      emptyText="none"
    />,
  );
}

describe("media rows", () => {
  it("shows a movie with year, size, watchers, torrent count and the seeding badge", () => {
    renderMedia([
      movie({ watchedBy: ["ann", "bob"], watchedAt: "2026-03-01T00:00:00Z", torrents: [torrent(), torrent({ id: "t2" })], belowSeedMin: true }),
    ]);
    const row = screen.getByRole("row", { name: /Alpha/ });
    expect(within(row).getByText("Alpha (1999)")).toBeInTheDocument();
    expect(within(row).getByText("4.0 GiB")).toBeInTheDocument();
    expect(within(row).getByText("ann, bob")).toBeInTheDocument();
    expect(within(row).getByText("2")).toBeInTheDocument();
    expect(within(row).getByText("Below seeding minimum")).toBeInTheDocument();
  });

  it("shows unknown for a missing watched date and a dash for no watchers", () => {
    renderMedia([movie()]);
    const row = screen.getByRole("row", { name: /Alpha/ });
    expect(within(row).getByText("unknown")).toBeInTheDocument();
    expect(within(row).getByText("—")).toBeInTheDocument();
  });

  it("shows the season and that the series is still airing", () => {
    renderMedia([movie({ list: "series", key: "series:1:season:2", title: "Show", scope: "season", season: 2, stillAiring: true })]);
    const row = screen.getByRole("row", { name: /Show/ });
    expect(within(row).getByText("Season 2")).toBeInTheDocument();
    expect(within(row).getByText("Still airing")).toBeInTheDocument();
  });

  it("shows a whole-series scope", () => {
    renderMedia([movie({ list: "series", key: "series:1", title: "Show", scope: "series" })]);
    expect(within(screen.getByRole("row", { name: /Show/ })).getByText("Whole series")).toBeInTheDocument();
  });

  it("names the other row a row is deleted together with", () => {
    renderMedia([movie({ tiedWith: [{ list: "movies", key: "movie:2", title: "Omega" }] })]);
    expect(screen.getByText("Deleted together with: Omega")).toBeInTheDocument();
  });

  it("shows the torrents of a row when expanded, with what lies outside the library", async () => {
    renderMedia([movie({ torrents: [torrent({ outsideLibrary: 6 * GiB })] })]);
    await userEvent.setup().click(screen.getByRole("button", { name: "Show torrents of Alpha" }));
    expect(screen.getByText("Some.Torrent.1080p")).toBeInTheDocument();
    expect(screen.getByText(/tracker\.example/)).toBeInTheDocument();
    expect(screen.getByText("Outside the library: 6.0 GiB")).toBeInTheDocument();
  });

  it("does not mention the outside-library size when the server sent none", async () => {
    renderMedia([movie({ torrents: [torrent()] })]);
    await userEvent.setup().click(screen.getByRole("button", { name: "Show torrents of Alpha" }));
    expect(screen.queryByText(/Outside the library/)).not.toBeInTheDocument();
  });
});

describe("torrent rows", () => {
  it("shows an unlinked torrent with its reason", () => {
    renderWithProviders(
      <CandidateList
        rows={[{ list: "torrents", key: "t1", title: "Some.Torrent.1080p", size: 8 * GiB, reason: "unlinked", torrents: [torrent()], belowSeedMin: false }]}
        columns={torrentColumns}
        sortKeys={torrentSortKeys}
        renderCard={torrentCard}
        actions={() => null}
        emptyText="none"
      />,
    );
    const row = screen.getByRole("row", { name: /Some\.Torrent/ });
    expect(within(row).getByText("Unlinked")).toBeInTheDocument();
    expect(within(row).getByText("tracker.example")).toBeInTheDocument();
    expect(within(row).getByText("1.50")).toBeInTheDocument();
  });
});
