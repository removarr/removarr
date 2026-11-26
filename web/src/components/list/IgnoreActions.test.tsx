import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it } from "vitest";
import type { Candidate, CandidatesResponse, Status } from "@/api/types";
import { ListPage } from "@/pages/ListPage";
import { mockApi, renderWithProviders } from "@/test/render";

beforeAll(() => {
  window.matchMedia = ((q: string) => ({
    matches: true, media: q, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {},
    onchange: null, dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

const c = { configured: true };
const status: Status = {
  dryRun: false, connections: { jellyfin: c, sonarr: c, radarr: c, qbittorrent: c },
  scan: { running: false }, lastScan: null, stale: false,
};
const movie: Candidate = { list: "movies", key: "tmdb:1", title: "Alpha", size: 1024 ** 3, torrents: [], belowSeedMin: false };
const live = (): CandidatesResponse => ({ list: "movies", scannedAt: "2026-03-01T00:00:00Z", candidates: [movie] });
const empty = (): CandidatesResponse => ({ list: "movies", scannedAt: "2026-03-01T00:00:00Z", candidates: [] });

const IGN = "GET /api/candidates?list=movies&ignored=true";
const LIVE = "GET /api/candidates?list=movies&ignored=false";
const ok = (body: unknown) => () => ({ status: 200, body });

describe("ignore", () => {
  it("sends only list and key, then reloads and clears the selection", async () => {
    let ignored = false;
    let body: unknown;
    const api = mockApi({
      "GET /api/status": ok(status),
      [LIVE]: () => ({ status: 200, body: ignored ? empty() : live() }),
      "POST /api/ignores": (init) => {
        body = JSON.parse(String(init.body));
        ignored = true;
        return { status: 204 };
      },
    });
    const user = userEvent.setup();
    renderWithProviders(<ListPage list="movies" />);
    await user.click(await screen.findByRole("checkbox", { name: "Select Alpha" }));
    await user.click(screen.getByRole("button", { name: "Ignore" }));
    await waitFor(() => expect(api.count(LIVE)).toBe(2));
    expect(body).toEqual({ rows: [{ list: "movies", key: "tmdb:1" }] });
    expect(await screen.findByText("Nothing to suggest.")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Selected rows" })).not.toBeInTheDocument();
  });

  it("shows the server's message and keeps the selection when ignoring fails", async () => {
    mockApi({
      "GET /api/status": ok(status),
      [LIVE]: ok(live()),
      "POST /api/ignores": () => ({ status: 400, body: { error: "tmdb:1 is not a row of the latest scan; reload the list" } }),
    });
    const user = userEvent.setup();
    renderWithProviders(<ListPage list="movies" />);
    await user.click(await screen.findByRole("checkbox", { name: "Select Alpha" }));
    await user.click(screen.getByRole("button", { name: "Ignore" }));
    expect(await screen.findByText(/is not a row of the latest scan/)).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Selected rows" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Select Alpha" })).toBeChecked();
  });
});

describe("ignored view", () => {
  const ignoredRows: Candidate[] = [
    { ...movie, ignoredAt: "2026-02-01T00:00:00Z" },
    { list: "movies", key: "tmdb:2", title: "Gone", size: 0, torrents: null, belowSeedMin: false, ignoredAt: "2026-02-02T00:00:00Z", notInScan: true },
    { ...movie, key: "tmdb:3", title: "Held", ignoredAt: "2026-02-03T00:00:00Z", waiting: true, waitReason: "needs a rescan" },
  ];

  async function openIgnored(extra = {}) {
    const api = mockApi({
      "GET /api/status": ok(status),
      [LIVE]: ok(live()),
      [IGN]: ok({ list: "movies", scannedAt: "2026-03-01T00:00:00Z", candidates: ignoredRows }),
      ...extra,
    });
    const user = userEvent.setup();
    renderWithProviders(<ListPage list="movies" bulkActions={() => <button>Delete</button>} />);
    await user.click(await screen.findByRole("checkbox", { name: "Select Alpha" }));
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
    await user.click(screen.getByRole("switch", { name: "Show ignored" }));
    return { api, user };
  }

  it("requests the ignored list, clears the selection and offers only Un-ignore", async () => {
    const { user } = await openIgnored();
    expect(await screen.findByText("Gone")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Selected rows" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Select Alpha" }));
    expect(screen.getByRole("button", { name: "Un-ignore" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Ignore" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
  });

  it("shows when a row was ignored, that it is not in the scan, and what it waits for", async () => {
    await openIgnored();
    expect(await screen.findByText("Not in the latest scan")).toBeInTheDocument();
    expect(screen.getAllByText(/^Ignored /).length).toBe(3);
    expect(screen.getByText("Waiting: needs a rescan")).toBeInTheDocument();
  });

  it("un-ignores with DELETE", async () => {
    let body: unknown;
    const { user } = await openIgnored({
      "DELETE /api/ignores": (init: RequestInit) => {
        body = JSON.parse(String(init.body));
        return { status: 204 };
      },
    });
    await user.click(await screen.findByRole("checkbox", { name: "Select Gone" }));
    await user.click(screen.getByRole("button", { name: "Un-ignore" }));
    await waitFor(() => expect(body).toEqual({ rows: [{ list: "movies", key: "tmdb:2" }] }));
  });

  it("says when nothing is ignored", async () => {
    mockApi({
      "GET /api/status": ok(status),
      [LIVE]: ok(live()),
      [IGN]: ok({ list: "movies", scannedAt: "2026-03-01T00:00:00Z", candidates: [] }),
    });
    const user = userEvent.setup();
    renderWithProviders(<ListPage list="movies" />);
    await user.click(await screen.findByRole("switch", { name: "Show ignored" }));
    expect(await screen.findByText("Nothing is ignored in this list.")).toBeInTheDocument();
  });
});
