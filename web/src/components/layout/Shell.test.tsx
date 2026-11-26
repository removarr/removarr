import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Settings } from "@/api/types";
import { mockApi, renderApp } from "@/test/render";

const KEY = "removarr.sidebar";

const settings: Settings = {
  rules: {
    countingUsers: [], watchedMode: "all", graceDays: 3, seriesUnit: "series", protectFavorites: true,
    protectionTag: "keep", minRatio: 0, minSeedDays: 0, excludedCategories: [], excludedTags: [],
    addExclusionOnDelete: false,
  },
  connections: { jellyfin: {}, sonarr: {}, radarr: {}, qbittorrent: {} },
  dryRun: false,
};

beforeEach(() => {
  localStorage.clear();
  const list = (name: string) => () => ({ status: 200, body: { list: name, scannedAt: null, candidates: null } });
  mockApi({
    "GET /api/actions?limit=50": () => ({ status: 200, body: { actions: [] } }),
    "GET /api/candidates?list=movies&ignored=false": list("movies"),
    "GET /api/candidates?list=series&ignored=false": list("series"),
    "GET /api/candidates?list=torrents&ignored=false": list("torrents"),
    "GET /api/settings": () => ({ status: 200, body: settings }),
    "GET /api/media-server/users": () => ({ status: 200, body: [] }),
    "GET /api/status": () => ({
      status: 200,
      body: { dryRun: false, connections: {}, scan: { running: false }, lastScan: null, stale: false },
    }),
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

// narrow makes every min-width query fail, as on a phone.
function narrow() {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: false, media: query, onchange: null,
    addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false,
  }));
}

const menu = () => screen.queryByRole("navigation", { name: "Main" });
const toggle = () => screen.getByRole("button", { name: /^(Hide|Show) menu$/ });

describe("the side panel on a wide screen", () => {
  it("is shown by default, and the toggle hides and shows it", async () => {
    const user = userEvent.setup();
    renderApp("/movies");
    expect(menu()).toBeInTheDocument();
    const button = screen.getByRole("button", { name: "Hide menu" });
    expect(button).toHaveAttribute("aria-expanded", "true");
    expect(button).toHaveAttribute("aria-controls", menu()!.id);

    await user.click(button);
    expect(menu()).not.toBeInTheDocument();
    expect(toggle()).toHaveAccessibleName("Show menu");
    expect(toggle()).toHaveAttribute("aria-expanded", "false");
    // The panel the toggle controls is still there, only hidden.
    expect(document.getElementById(toggle().getAttribute("aria-controls")!)).not.toBeNull();

    await user.click(toggle());
    expect(menu()).toBeInTheDocument();
    expect(toggle()).toHaveAccessibleName("Hide menu");
    expect(toggle()).toHaveAttribute("aria-expanded", "true");
  });

  it("remembers that the panel was hidden", async () => {
    const user = userEvent.setup();
    const first = renderApp("/movies");
    await user.click(screen.getByRole("button", { name: "Hide menu" }));
    expect(localStorage.getItem(KEY)).toBe("closed");
    first.unmount();

    renderApp("/movies");
    expect(menu()).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show menu" })).toBeInTheDocument();
  });

  it("still works when the browser's storage throws", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("denied"); });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("denied"); });
    const user = userEvent.setup();
    renderApp("/movies");
    expect(menu()).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Hide menu" }));
    expect(menu()).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    expect(menu()).toBeInTheDocument();
  });

  it("does not close when a link is chosen", async () => {
    const user = userEvent.setup();
    renderApp("/movies");
    await user.click(within(menu()!).getByRole("link", { name: "Series" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Series" })).toBeInTheDocument();
    expect(menu()).toBeInTheDocument();
  });
});

describe("the scan details strip", () => {
  it("sits below the header bar, beside the side panel and above the page", async () => {
    const { container } = renderApp("/movies");
    const details = await screen.findByTestId("scan-details");
    const header = container.querySelector("header")!;
    const strip = screen.getByTestId("scan-strip");
    const main = container.querySelector("main")!;
    expect(strip).toContainElement(details);
    expect(header).not.toContainElement(details);
    expect(within(header).queryByText(/No scan yet|Last scan/)).not.toBeInTheDocument();
    expect(header.compareDocumentPosition(strip) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(strip.compareDocumentPosition(main) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // Beside the panel: in the content column with the page, not in or under the panel.
    expect(menu()).not.toContainElement(strip);
    expect(strip.parentElement).toBe(main.parentElement);
    expect(within(details).getByRole("button", { name: /^No scan yet/ })).toHaveAttribute("aria-expanded", "true");
  });
});

// panel is the element the toggle controls, found whether it is open or closed.
const panel = () => document.getElementById(toggle().getAttribute("aria-controls")!)!;

describe("the side panel's open and closed states", () => {
  it("keeps the closed panel out of reach and the open one in reach, on a wide screen", async () => {
    const user = userEvent.setup();
    renderApp("/movies");
    expect(panel()).not.toHaveAttribute("inert");
    expect(panel()).not.toHaveAttribute("aria-hidden");
    expect(panel().style.width).toBe("14rem");
    await user.click(screen.getByRole("button", { name: "Hide menu" }));
    // Still there for aria-controls, but inert (no link can take the focus) and hidden from
    // assistive technology; CSS hides it once its closing animation ended.
    expect(panel()).toBeInTheDocument();
    expect(panel()).toHaveAttribute("inert");
    expect(panel()).toHaveAttribute("aria-hidden", "true");
    expect(panel()).toHaveClass("invisible");
    // No border either, or the closed panel would still take a pixel from the content.
    expect(panel()).toHaveClass("border-r-0");
    expect(panel().style.width).toBe("0px");
    expect(panel().querySelectorAll("a").length).toBeGreaterThan(0);
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    expect(panel()).not.toHaveAttribute("inert");
    expect(panel()).toHaveClass("visible");
  });

  it("keeps the closed drawer out of reach and the open one in reach, on a narrow screen", async () => {
    narrow();
    const user = userEvent.setup();
    renderApp("/movies");
    expect(panel()).toHaveAttribute("inert");
    expect(panel()).toHaveAttribute("aria-hidden", "true");
    expect(panel()).toHaveClass("-translate-x-full", "invisible");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    expect(panel()).not.toHaveAttribute("inert");
    expect(panel()).not.toHaveAttribute("aria-hidden");
    expect(panel()).toHaveClass("translate-x-0", "visible");
  });

  it("animates the width on a wide screen, and not when reduced motion is asked for", () => {
    renderApp("/movies");
    expect(panel().className).toMatch(/transition-\[width/);
    expect(panel()).toHaveClass("duration-200", "ease-out", "motion-reduce:transition-none");
  });

  it("slides the drawer and fades the backdrop on a narrow screen, and not with reduced motion", async () => {
    narrow();
    const user = userEvent.setup();
    renderApp("/movies");
    expect(panel().className).toMatch(/transition-\[transform/);
    expect(panel()).toHaveClass("duration-200", "ease-out", "motion-reduce:transition-none");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    const backdrop = screen.getByTestId("menu-backdrop");
    expect(backdrop.className).toMatch(/transition-\[opacity/);
    expect(backdrop).toHaveClass("opacity-100", "duration-200", "motion-reduce:transition-none");
  });

  it("moves the bulk bar's left edge with the panel", () => {
    renderApp("/movies");
    const shell = panel().closest("[style*='--content-left']") as HTMLElement;
    expect(shell.style.getPropertyValue("--content-left")).toBe("14rem");
  });
});

describe("the side panel on a narrow screen", () => {
  it("is hidden by default and opens as a drawer over the content", async () => {
    narrow();
    const user = userEvent.setup();
    renderApp("/movies");
    expect(menu()).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show menu" })).toHaveAttribute("aria-expanded", "false");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    expect(menu()).toBeInTheDocument();
    expect(screen.getByTestId("menu-backdrop")).toBeInTheDocument();
    expect(toggle()).toHaveAttribute("aria-expanded", "true");
  });

  it("closes on Escape and gives the focus back to the toggle", async () => {
    narrow();
    const user = userEvent.setup();
    renderApp("/movies");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    await user.keyboard("{Escape}");
    expect(menu()).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show menu" })).toHaveFocus();
  });

  it("closes on a click on the backdrop", async () => {
    narrow();
    const user = userEvent.setup();
    renderApp("/movies");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    await user.click(screen.getByTestId("menu-backdrop"));
    expect(menu()).not.toBeInTheDocument();
    // The backdrop stays mounted so that it can fade out; closed, it takes no click.
    expect(screen.getByTestId("menu-backdrop")).toHaveClass("opacity-0", "pointer-events-none", "invisible");
    expect(screen.getByRole("button", { name: "Show menu" })).toHaveFocus();
  });

  it("makes the header, the scan details and the page inert while the drawer is open", async () => {
    narrow();
    const user = userEvent.setup();
    const { container } = renderApp("/movies");
    const header = container.querySelector("header")!;
    const main = container.querySelector("main")!;
    const strip = screen.getByTestId("scan-strip");
    expect(header).not.toHaveAttribute("inert");
    expect(main).not.toHaveAttribute("inert");
    expect(strip).not.toHaveAttribute("inert");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    expect(header).toHaveAttribute("inert");
    expect(main).toHaveAttribute("inert");
    expect(strip).toHaveAttribute("inert");
    await user.keyboard("{Escape}");
    expect(header).not.toHaveAttribute("inert");
    expect(main).not.toHaveAttribute("inert");
    expect(strip).not.toHaveAttribute("inert");
  });

  it("keeps the focus in the drawer when tabbing past its last or first link", async () => {
    narrow();
    const user = userEvent.setup();
    renderApp("/movies");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    const links = within(menu()!).getAllByRole("link");
    expect(links[0]).toHaveFocus();
    for (let i = 1; i < links.length; i++) await user.tab();
    expect(links[links.length - 1]).toHaveFocus();
    await user.tab();
    expect(menu()).toContainElement(document.activeElement as HTMLElement);
    expect(links[0]).toHaveFocus();
    await user.tab({ shift: true });
    expect(links[links.length - 1]).toHaveFocus();
  });

  it("closes when a link is chosen and shows its page", async () => {
    narrow();
    const user = userEvent.setup();
    renderApp("/movies");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    await user.click(within(menu()!).getByRole("link", { name: "History" }));
    expect(await screen.findByRole("heading", { level: 1, name: "History" })).toBeInTheDocument();
    expect(menu()).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show menu" })).toHaveFocus();
  });

  it("does not remember the drawer", async () => {
    narrow();
    const user = userEvent.setup();
    const first = renderApp("/movies");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    expect(localStorage.getItem(KEY)).toBeNull();
    first.unmount();
    renderApp("/movies");
    expect(menu()).not.toBeInTheDocument();
  });
});

describe("the menu items", () => {
  it("names each link by its label only, with the icon hidden", () => {
    renderApp("/movies");
    const links = within(menu()!).getAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual([
      "Movies", "Series", "Torrents", "History", "Settings", "Connections", "Counting users", "Rules",
    ]);
    for (const link of links) {
      const icon = link.querySelector("svg");
      expect(icon, `icon of ${link.textContent}`).not.toBeNull();
      expect(icon).toHaveAttribute("aria-hidden", "true");
      expect(link).toHaveAccessibleName(link.textContent!);
    }
  });

  it("nests the settings pages below Settings", () => {
    renderApp("/movies");
    const settings = within(menu()!).getByRole("link", { name: "Settings" });
    const sub = within(settings.closest("li")!).getAllByRole("link").slice(1);
    expect(sub.map((l) => l.textContent)).toEqual(["Connections", "Counting users", "Rules"]);
  });

  it.each([
    ["/settings/connections", "Connections"],
    ["/settings/counting-users", "Counting users"],
    ["/settings/rules", "Rules"],
  ])("marks %s as the current page and Settings as current", async (path, name) => {
    renderApp(path);
    expect(await screen.findByRole("heading", { level: 1, name })).toBeInTheDocument();
    const nav = menu()!;
    expect(within(nav).getByRole("link", { name })).toHaveAttribute("aria-current", "page");
    expect(within(nav).getByRole("link", { name: "Settings" })).toHaveAttribute("aria-current", "true");
    expect(nav.querySelectorAll('[aria-current="page"]')).toHaveLength(1);
  });

  it("sends /settings to the connections", async () => {
    const { router } = renderApp("/settings");
    expect(await screen.findByRole("heading", { level: 1, name: "Connections" })).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/connections"));
  });
});
