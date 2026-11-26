import { useQuery } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Status } from "@/api/types";
import { formatDateTime } from "@/lib/format";
import { mockApi, renderWithProviders } from "@/test/render";
import { Header } from "./Header";

const FINISHED = "2026-11-20T10:30:00Z";

function status(over: Partial<Status> = {}): Status {
  return {
    dryRun: false,
    connections: {
      jellyfin: { configured: true }, sonarr: { configured: true },
      radarr: { configured: true }, qbittorrent: { configured: true },
    },
    scan: { running: false },
    stale: false,
    lastScan: {
      id: 1, startedAt: "2026-11-20T10:29:00Z", finishedAt: FINISHED, status: "done",
      summary: {
        lists: {
          movies: { candidates: 12, protected: 1, unmatched: 2, waiting: 3 },
          series: { candidates: 4, protected: 2, unmatched: 0, waiting: 1 },
          torrents: { candidates: 9, protected: 0, unmatched: 1, waiting: 0 },
        },
        rootFolders: [{ path: "/media/movies", freeBytes: 2 * 1024 ** 3 }],
      },
    },
    ...over,
  };
}

describe("the header", () => {
  it("shows the last scan, the summary and the free space", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    renderWithProviders(<Header />);
    expect(await screen.findByText(`Last scan ${formatDateTime(FINISHED)}`)).toBeInTheDocument();
    expect(screen.getByTestId("counts")).toHaveTextContent("Movies 12 · Series 4 · Torrents 9");
    expect(screen.getByText("3 protected")).toBeInTheDocument();
    expect(screen.getByText("3 unmatched")).toBeInTheDocument();
    expect(screen.getByText("4 waiting")).toBeInTheDocument();
    expect(screen.getByText("/media/movies")).toBeInTheDocument();
    expect(screen.getByText("2.0 GiB")).toBeInTheDocument();
  });

  it("says so when there is no scan yet", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ lastScan: null }) }) });
    renderWithProviders(<Header />);
    expect(await screen.findByText("No scan yet")).toBeInTheDocument();
    expect(screen.queryByTestId("counts")).not.toBeInTheDocument();
  });

  it("shows a list that failed as an error with its message", async () => {
    const s = status();
    s.lastScan!.summary.lists.movies = { candidates: 0, protected: 0, unmatched: 0, waiting: 0, error: "radarr is down" };
    mockApi({ "GET /api/status": () => ({ status: 200, body: s }) });
    renderWithProviders(<Header />);
    const counts = await screen.findByTestId("counts");
    expect(counts).toHaveTextContent("Movies: error · Series 4 · Torrents 9");
    expect(within(counts).getByText("Movies: error")).toHaveAccessibleDescription("radarr is down");
  });

  it("shows the last scan's error", async () => {
    mockApi({
      "GET /api/status": () => ({ status: 200, body: status({ scan: { running: false, lastError: "jellyfin refused" } }) }),
    });
    renderWithProviders(<Header />);
    expect(await screen.findByText("jellyfin refused")).toBeInTheDocument();
  });

  it("shows the stale notice only when the settings changed", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ stale: true }) }) });
    const first = renderWithProviders(<Header />);
    expect(await screen.findByText("Settings changed since the last scan. Scan again to update the lists.")).toBeInTheDocument();
    first.unmount();

    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    renderWithProviders(<Header />);
    await screen.findByText(/Last scan/);
    expect(screen.queryByText(/Settings changed/)).not.toBeInTheDocument();
  });

  it("shows the dry run badge only in dry run", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ dryRun: true }) }) });
    const first = renderWithProviders(<Header />);
    expect(await screen.findByText("Dry run")).toBeInTheDocument();
    first.unmount();

    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    renderWithProviders(<Header />);
    await screen.findByText(/Last scan/);
    expect(screen.queryByText("Dry run")).not.toBeInTheDocument();
  });

  it("starts a scan with a POST when Scan is clicked", async () => {
    const m = mockApi({
      "GET /api/status": () => ({ status: 200, body: status() }),
      "POST /api/scans": () => ({ status: 202, body: { status: { running: true }, last: null } }),
    });
    renderWithProviders(<Header />);
    await userEvent.click(await screen.findByRole("button", { name: "Scan" }));
    await waitFor(() => expect(m.count("POST /api/scans")).toBe(1));
    const post = m.fetchMock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === "POST");
    expect(((post![1] as RequestInit).headers as Record<string, string>)["X-Removarr-Request"]).toBe("1");
  });

  it("shows the server's message when the start is refused and does not retry", async () => {
    const m = mockApi({
      "GET /api/status": () => ({ status: 200, body: status() }),
      "POST /api/scans": () => ({ status: 409, body: { error: "a scan is running" } }),
    });
    renderWithProviders(<Header />);
    await userEvent.click(await screen.findByRole("button", { name: "Scan" }));
    expect(await screen.findByText("a scan is running")).toBeInTheDocument();
    expect(m.count("POST /api/scans")).toBe(1);
  });

  it("reads the status again once after a refused start, so the header learns what runs", async () => {
    let running = false;
    const m = mockApi({
      "GET /api/status": () => ({ status: 200, body: status({ scan: { running, phase: "reading" } }) }),
      "POST /api/scans": () => {
        running = true;
        return { status: 409, body: { error: "a scan is running" } };
      },
    });
    renderWithProviders(<Header />);
    await userEvent.click(await screen.findByRole("button", { name: "Scan" }));
    expect(await screen.findByRole("button", { name: /Scanning/ })).toBeDisabled();
    expect(m.count("GET /api/status")).toBe(2);
    expect(m.count("POST /api/scans")).toBe(1);
  });

  it("clears the refusal message when the scan state changes", async () => {
    let running = false;
    mockApi({
      "GET /api/status": () => ({ status: 200, body: status({ scan: { running, phase: "reading" } }) }),
      "POST /api/scans": () => ({ status: 409, body: { error: "plan is being built" } }),
    });
    const { queryClient } = renderWithProviders(<Header />);
    await userEvent.click(await screen.findByRole("button", { name: "Scan" }));
    expect(await screen.findByText("plan is being built")).toBeInTheDocument();
    running = true;
    await queryClient.refetchQueries({ queryKey: ["status"] });
    await screen.findByRole("button", { name: /Scanning/ });
    expect(screen.queryByText("plan is being built")).not.toBeInTheDocument();
  });

  it("disables the button and shows the phase while a scan runs", async () => {
    mockApi({
      "GET /api/status": () => ({ status: 200, body: status({ scan: { running: true, phase: "reading jellyfin" } }) }),
    });
    renderWithProviders(<Header />);
    const button = await screen.findByRole("button", { name: /Scanning/ });
    expect(button).toBeDisabled();
    expect(screen.getByText("reading jellyfin")).toBeInTheDocument();
  });

  it("reloads the lists when a scan finishes", async () => {
    let running = true;
    const m = mockApi({
      "GET /api/status": () => ({ status: 200, body: status({ scan: { running, phase: "reading" } }) }),
      "GET /api/candidates": () => ({ status: 200, body: {} }),
    });
    function Probe() {
      useQuery({ queryKey: ["candidates", "movies", false], queryFn: () => fetch("/api/candidates").then((r) => r.json()) });
      return null;
    }
    const { queryClient } = renderWithProviders(
      <>
        <Header />
        <Probe />
      </>,
    );
    await screen.findByRole("button", { name: /Scanning/ });
    await waitFor(() => expect(m.count("GET /api/candidates")).toBe(1));

    running = false;
    await queryClient.refetchQueries({ queryKey: ["status"] });
    await screen.findByRole("button", { name: "Scan" });
    await waitFor(() => expect(m.count("GET /api/candidates")).toBe(2));
  });
});
