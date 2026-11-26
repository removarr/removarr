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
    expect(screen.queryByTestId("menu-backdrop")).not.toBeInTheDocument();
  });

  it("closes when a link is chosen and shows its page", async () => {
    narrow();
    const user = userEvent.setup();
    renderApp("/movies");
    await user.click(screen.getByRole("button", { name: "Show menu" }));
    await user.click(within(menu()!).getByRole("link", { name: "History" }));
    expect(await screen.findByRole("heading", { level: 1, name: "History" })).toBeInTheDocument();
    expect(menu()).not.toBeInTheDocument();
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
