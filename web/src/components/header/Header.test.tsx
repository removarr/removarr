import { useQuery } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Status } from "@/api/types";
import { formatDateTime } from "@/lib/format";
import { SCAN_FINISHED, scannedStatus as status } from "@/test/fixtures";
import { mockApi, renderWithProviders } from "@/test/render";
import { Header } from "./Header";
import { ScanDetails } from "./ScanDetails";

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("the header's layout", () => {
  // The bar holds the lead (menu button and name) and the controls only; the scan's details
  // are a strip of their own below it (ScanDetails).
  it("holds the name and the controls only, with Scan the last control", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ dryRun: true, stale: true }) }) });
    renderWithProviders(<Header lead={<span>removarr</span>} />);
    const scan = await screen.findByRole("button", { name: "Scan" });
    const controls = screen.getByTestId("scan-controls");
    const row = controls.parentElement!;
    expect(Array.from(row.children)).toEqual([screen.getByText("removarr").parentElement, controls]);
    expect(scan.parentElement!.lastElementChild).toBe(scan);
    expect(within(scan.parentElement!).getByText("Dry run")).toBeInTheDocument();
    expect(controls).toContainElement(scan);
    expect(controls).toHaveClass("ml-auto");
    expect(screen.queryByTestId("scan-details")).not.toBeInTheDocument();
    expect(screen.queryByText(/Last scan/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Settings changed/)).not.toBeInTheDocument();
  });

  it("keeps the refusal message and the phase with the controls, after the Scan button", async () => {
    mockApi({
      "GET /api/status": () => ({ status: 200, body: status() }),
      "POST /api/scans": () => ({ status: 409, body: { error: "a scan is running" } }),
    });
    renderWithProviders(<Header lead={<span>removarr</span>} />);
    await userEvent.click(await screen.findByRole("button", { name: "Scan" }));
    const alert = await screen.findByRole("alert");
    const controls = screen.getByTestId("scan-controls");
    expect(controls).toContainElement(alert);
    const scan = screen.getByRole("button", { name: "Scan" });
    expect(scan.compareDocumentPosition(alert) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});

describe("the header", () => {
  it("shows the dry run badge only in dry run", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ dryRun: true }) }) });
    const first = renderWithProviders(<Header />);
    expect(await screen.findByText("Dry run")).toBeInTheDocument();
    first.unmount();

    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    // The details show once the status is read, which is when the badge would show.
    renderWithProviders(<><Header /><ScanDetails /></>);
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

  it("reloads the lists when a scan finished before the status showed it running", async () => {
    // A quick scan is over by the time the status is read again: the status never shows it
    // running, only a new last scan.
    let last: Status["lastScan"] = null;
    const m = mockApi({
      "GET /api/status": () => ({ status: 200, body: status({ lastScan: last }) }),
      "POST /api/scans": () => {
        last = status().lastScan;
        return { status: 202, body: { status: { running: true }, last: null } };
      },
      "GET /api/candidates": () => ({ status: 200, body: {} }),
    });
    function Probe() {
      useQuery({ queryKey: ["candidates", "movies", false], queryFn: () => fetch("/api/candidates").then((r) => r.json()) });
      return null;
    }
    renderWithProviders(
      <>
        <Header />
        <ScanDetails />
        <Probe />
      </>,
    );
    expect(await screen.findByText("No scan yet")).toBeInTheDocument();
    await waitFor(() => expect(m.count("GET /api/candidates")).toBe(1));

    await userEvent.click(screen.getByRole("button", { name: "Scan" }));
    expect(await screen.findByText(`Last scan ${formatDateTime(SCAN_FINISHED)}`)).toBeInTheDocument();
    await waitFor(() => expect(m.count("GET /api/candidates")).toBe(2));
  });
});
