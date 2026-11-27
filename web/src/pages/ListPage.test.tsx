import { screen } from "@testing-library/react";
import { beforeAll, describe, expect, it } from "vitest";
import type { Candidate, CandidatesResponse, Status } from "@/api/types";
import { mockApi, renderWithProviders } from "@/test/render";
import { ListPage } from "./ListPage";

beforeAll(() => {
  window.matchMedia = ((q: string) => ({
    matches: true, media: q, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {},
    onchange: null, dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

function status(configured: boolean): Status {
  const c = { configured };
  return {
    dryRun: false, connections: { jellyfin: c, sonarr: c, radarr: c, qbittorrent: c },
    scan: { running: false }, lastScan: null, stale: false,
  };
}

function routes(s: Status, c: CandidatesResponse) {
  return {
    "GET /api/status": () => ({ status: 200, body: s }),
    "GET /api/candidates?list=movies&ignored=false": () => ({ status: 200, body: c }),
  };
}

const movie: Candidate = { list: "movies", key: "movie:1", title: "Alpha", year: 1999, size: 1024 ** 3, torrents: [], belowSeedMin: false };

describe("ListPage", () => {
  it("points to the settings on first run", async () => {
    mockApi(routes(status(false), { list: "movies", scannedAt: null, candidates: null }));
    renderWithProviders(<ListPage list="movies" />);
    expect(await screen.findByText("removarr is not connected to any app yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /settings/i })).toHaveAttribute("href", "/settings/connections");
  });

  it("shows the list's error, no rows and no select-all box", async () => {
    mockApi(routes(status(true), { list: "movies", scannedAt: "2026-03-01T00:00:00Z", error: "radarr could not be read", candidates: [movie] }));
    renderWithProviders(<ListPage list="movies" />);
    expect(await screen.findByRole("alert")).toHaveTextContent("radarr could not be read");
    expect(screen.queryByText("Alpha (1999)")).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Select all shown rows" })).not.toBeInTheDocument();
  });

  it("says no scan has run yet", async () => {
    mockApi(routes(status(true), { list: "movies", scannedAt: null, candidates: null }));
    renderWithProviders(<ListPage list="movies" />);
    expect(await screen.findByText("No scan yet. Start one with Scan above.")).toBeInTheDocument();
  });

  it("shows the rows", async () => {
    mockApi(routes(status(true), { list: "movies", scannedAt: "2026-03-01T00:00:00Z", candidates: [movie] }));
    renderWithProviders(<ListPage list="movies" />);
    expect(await screen.findByText("Alpha (1999)")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Select all shown rows" })).toBeInTheDocument();
  });

  it("says there is nothing to suggest for a null list", async () => {
    mockApi(routes(status(true), { list: "movies", scannedAt: "2026-03-01T00:00:00Z", candidates: null }));
    renderWithProviders(<ListPage list="movies" />);
    expect(await screen.findByText("Nothing to suggest.")).toBeInTheDocument();
  });

  it("shows a request error in an alert", async () => {
    mockApi({
      "GET /api/status": () => ({ status: 200, body: status(true) }),
      "GET /api/candidates?list=movies&ignored=false": () => ({ status: 500, body: { error: "store is broken" } }),
    });
    renderWithProviders(<ListPage list="movies" />);
    expect(await screen.findByRole("alert")).toHaveTextContent("store is broken");
  });
});
