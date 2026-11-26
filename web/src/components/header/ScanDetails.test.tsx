import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Status } from "@/api/types";
import { formatDateTime } from "@/lib/format";
import { SCAN_FINISHED, scannedStatus as status } from "@/test/fixtures";
import { mockApi, renderWithProviders } from "@/test/render";
import { ScanDetails } from "./ScanDetails";

beforeEach(() => localStorage.clear());
afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

const DETAILS_KEY = "removarr.scanDetails";
const LAST_SCAN = `Last scan ${formatDateTime(SCAN_FINISHED)}`;
// line is the strip's first line: one button that collapses and expands the details.
const line = () => screen.getByTestId("scan-summary");
// detailsBody is the part the line collapses, found through its aria-controls.
const detailsBody = () => document.getElementById(line().getAttribute("aria-controls")!)!;

describe("the scan details strip", () => {
  it("shows the details by default, and its first line collapses and expands them", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    const user = userEvent.setup();
    renderWithProviders(<ScanDetails />);
    await screen.findByTestId("counts");
    // One button, named by its visible text, with the chevron last.
    const button = screen.getByRole("button", { name: new RegExp(`^${LAST_SCAN}`) });
    expect(button).toBe(line());
    expect(screen.getAllByRole("button")).toEqual([button]);
    expect(button).toHaveAttribute("type", "button");
    expect(button).not.toHaveAttribute("aria-label");
    expect(button).toHaveAttribute("aria-expanded", "true");
    const chevron = button.lastElementChild!;
    expect(chevron.tagName.toLowerCase()).toBe("svg");
    expect(chevron).toHaveAttribute("aria-hidden", "true");
    const body = detailsBody();
    expect(body).toContainElement(screen.getByTestId("counts"));
    expect(button).not.toContainElement(screen.getByTestId("counts"));
    expect(body).not.toHaveAttribute("inert");
    expect(body).not.toHaveAttribute("aria-hidden");

    // A click on the text, not on the chevron, collapses the details to the line.
    await user.click(within(button).getByText(LAST_SCAN));
    expect(button).toHaveAttribute("aria-expanded", "false");
    expect(body).toBeInTheDocument();
    expect(body).toHaveAttribute("inert");
    expect(body).toHaveAttribute("aria-hidden", "true");
    expect(body).toHaveClass("invisible", "grid-rows-[0fr]");
    expect(line()).toHaveTextContent(LAST_SCAN);
    expect(screen.queryByTestId("attention")).not.toBeInTheDocument();

    await user.click(within(button).getByText(LAST_SCAN));
    expect(button).toHaveAttribute("aria-expanded", "true");
    expect(body).not.toHaveAttribute("inert");
    expect(body).toHaveClass("visible", "grid-rows-[1fr]");
  });

  it("toggles with Enter and Space", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    const user = userEvent.setup();
    renderWithProviders(<ScanDetails />);
    await screen.findByTestId("counts");
    line().focus();
    await user.keyboard("{Enter}");
    expect(line()).toHaveAttribute("aria-expanded", "false");
    await user.keyboard(" ");
    expect(line()).toHaveAttribute("aria-expanded", "true");
  });

  it("shows a focus ring around the whole line", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    renderWithProviders(<ScanDetails />);
    await screen.findByTestId("counts");
    expect(line()).toHaveClass("w-full", "focus-visible:ring-[3px]", "focus-visible:ring-ring/50");
  });

  it("animates the collapse, and not when reduced motion is asked for", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    renderWithProviders(<ScanDetails />);
    await screen.findByTestId("counts");
    const body = detailsBody();
    expect(body.className).toMatch(/transition-\[grid-template-rows/);
    expect(body).toHaveClass("duration-200", "ease-out", "motion-reduce:transition-none");
  });

  it("puts the details on a surface of their own", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    renderWithProviders(<ScanDetails />);
    await screen.findByTestId("counts");
    expect(screen.getByTestId("scan-details")).toHaveClass("bg-muted/50", "rounded-md");
  });

  it("remembers that the details were collapsed", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    const user = userEvent.setup();
    const first = renderWithProviders(<ScanDetails />);
    await screen.findByTestId("counts");
    await user.click(line());
    expect(localStorage.getItem(DETAILS_KEY)).toBe("closed");
    first.unmount();

    renderWithProviders(<ScanDetails />);
    expect(await screen.findByTestId("scan-summary")).toHaveAttribute("aria-expanded", "false");
    expect(detailsBody()).toHaveAttribute("inert");
    await user.click(line());
    expect(localStorage.getItem(DETAILS_KEY)).toBe("open");
  });

  it("still works when the browser's storage throws", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("denied"); });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("denied"); });
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    const user = userEvent.setup();
    renderWithProviders(<ScanDetails />);
    await screen.findByTestId("counts");
    expect(line()).toHaveAttribute("aria-expanded", "true");
    await user.click(line());
    expect(line()).toHaveAttribute("aria-expanded", "false");
    expect(detailsBody()).toHaveAttribute("inert");
    await user.click(line());
    expect(line()).toHaveAttribute("aria-expanded", "true");
    expect(detailsBody()).not.toHaveAttribute("inert");
  });

  it.each([
    ["the lists are out of date", { stale: true }, "Needs attention: lists out of date"],
    ["the last scan failed", { scan: { running: false, lastError: "jellyfin refused" } }, "Needs attention: the last scan failed"],
  ] as [string, Partial<Status>, string][])("flags a problem in the line when %s, also collapsed", async (_, over, text) => {
    localStorage.setItem(DETAILS_KEY, "closed");
    mockApi({ "GET /api/status": () => ({ status: 200, body: status(over) }) });
    renderWithProviders(<ScanDetails />);
    const mark = await screen.findByTestId("attention");
    expect(line()).toContainElement(mark);
    expect(mark).toHaveTextContent(text);
    expect(mark.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
    // The reasons are part of the button's name, after the visible text.
    expect(line()).toHaveAccessibleName(new RegExp(`^${LAST_SCAN}.*${text}`));
  });

  it("flags a list that could not be produced in the line", async () => {
    localStorage.setItem(DETAILS_KEY, "closed");
    const s = status();
    s.lastScan!.summary.lists.series = { candidates: 0, protected: 0, unmatched: 0, waiting: 0, error: "sonarr is down" };
    mockApi({ "GET /api/status": () => ({ status: 200, body: s }) });
    renderWithProviders(<ScanDetails />);
    expect(await screen.findByTestId("attention")).toHaveTextContent("Needs attention: a list could not be produced");
  });

  it("names every problem at once", async () => {
    localStorage.setItem(DETAILS_KEY, "closed");
    const s = status({ stale: true, scan: { running: false, lastError: "jellyfin refused" } });
    s.lastScan!.summary.lists.series = { candidates: 0, protected: 0, unmatched: 0, waiting: 0, error: "sonarr is down" };
    mockApi({ "GET /api/status": () => ({ status: 200, body: s }) });
    renderWithProviders(<ScanDetails />);
    expect(await screen.findByTestId("attention")).toHaveTextContent(
      "Needs attention: lists out of date, a list could not be produced, the last scan failed",
    );
  });

  it("says when there is no scan yet, and when one runs", async () => {
    localStorage.setItem(DETAILS_KEY, "closed");
    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ lastScan: null }) }) });
    const first = renderWithProviders(<ScanDetails />);
    expect(await screen.findByRole("button", { name: /^No scan yet/ })).toBe(line());
    first.unmount();

    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ scan: { running: true, phase: "reading" } }) }) });
    renderWithProviders(<ScanDetails />);
    expect(await screen.findByTestId("scan-summary")).toHaveTextContent("Scanning…");
  });
});

describe("the scan details", () => {
  it("show the last scan, the summary and the free space", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    renderWithProviders(<ScanDetails />);
    expect(await screen.findByText(LAST_SCAN)).toBeInTheDocument();
    expect(screen.getByTestId("counts")).toHaveTextContent("Movies 12 · Series 4 · Torrents 9");
    expect(screen.getByText("3 protected")).toBeInTheDocument();
    expect(screen.getByText("3 unmatched")).toBeInTheDocument();
    expect(screen.getByText("4 waiting")).toBeInTheDocument();
    expect(screen.getByText("/media/movies")).toBeInTheDocument();
    expect(screen.getByText("2.0 GiB")).toBeInTheDocument();
  });

  it("say so when there is no scan yet", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ lastScan: null }) }) });
    renderWithProviders(<ScanDetails />);
    expect(await screen.findByText("No scan yet")).toBeInTheDocument();
    expect(screen.queryByTestId("counts")).not.toBeInTheDocument();
  });

  it("show a list that failed as an error with its message", async () => {
    const s = status();
    s.lastScan!.summary.lists.movies = { candidates: 0, protected: 0, unmatched: 0, waiting: 0, error: "radarr is down" };
    mockApi({ "GET /api/status": () => ({ status: 200, body: s }) });
    renderWithProviders(<ScanDetails />);
    const counts = await screen.findByTestId("counts");
    expect(counts).toHaveTextContent("Movies: error · Series 4 · Torrents 9");
    expect(within(counts).getByText("Movies: error")).toHaveAccessibleDescription("radarr is down");
  });

  it("show the last scan's error", async () => {
    mockApi({
      "GET /api/status": () => ({ status: 200, body: status({ scan: { running: false, lastError: "jellyfin refused" } }) }),
    });
    renderWithProviders(<ScanDetails />);
    expect(await screen.findByText("jellyfin refused")).toBeInTheDocument();
  });

  it("show the stale notice only when the settings changed", async () => {
    mockApi({ "GET /api/status": () => ({ status: 200, body: status({ stale: true }) }) });
    const first = renderWithProviders(<ScanDetails />);
    expect(await screen.findByText("Settings changed since the last scan. Scan again to update the lists.")).toBeInTheDocument();
    first.unmount();

    mockApi({ "GET /api/status": () => ({ status: 200, body: status() }) });
    renderWithProviders(<ScanDetails />);
    await screen.findByText(/Last scan/);
    expect(screen.queryByText(/Settings changed/)).not.toBeInTheDocument();
  });
});
