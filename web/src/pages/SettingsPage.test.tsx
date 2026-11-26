import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Settings } from "@/api/types";
import { SettingsPage } from "@/pages/SettingsPage";
import { mockApi, renderWithProviders } from "@/test/render";

const SETTINGS = "GET /api/settings";
const PUT = "PUT /api/settings";
const USERS = "GET /api/media-server/users";

function settingsFixture(over: Partial<Settings> = {}): Settings {
  return {
    rules: {
      countingUsers: [], watchedMode: "all", graceDays: 3, seriesUnit: "series", protectFavorites: true,
      protectionTag: "keep", minRatio: 0, minSeedDays: 0, excludedCategories: [], excludedTags: [],
      addExclusionOnDelete: false,
    },
    connections: {
      jellyfin: { url: { value: "http://192.0.2.1:8096", set: true, source: "ui" }, apiKey: { set: true, source: "ui" } },
      sonarr: { url: { value: "http://192.0.2.1:8988", set: true, source: "ui" }, apiKey: { set: true, source: "env" } },
      radarr: { url: { value: "http://192.0.2.1:7878", set: true, source: "ui" }, apiKey: { set: true, source: "ui" } },
      qbittorrent: {
        url: { value: "http://192.0.2.1:8080", set: true, source: "env" },
        username: { value: "admin", set: true, source: "env" },
        password: { set: false, source: "none" },
      },
    },
    dryRun: true,
    ...over,
  };
}

const ok = (body: unknown) => () => ({ status: 200, body });
const bodyOf = (calls: { init: RequestInit }[]) => JSON.parse(String(calls[0].init.body));

// A route that records the body of each request.
function recorder(answer: (body: unknown) => { status: number; body?: unknown }) {
  const calls: { init: RequestInit }[] = [];
  return { calls, route: (init: RequestInit) => { calls.push({ init }); return answer(JSON.parse(String(init.body))); } };
}

describe("SettingsPage connections", () => {
  it("saving a changed URL sends that URL only", async () => {
    const put = recorder(() => ({ status: 200, body: settingsFixture() }));
    mockApi({ [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderWithProviders(<SettingsPage />);
    const sonarr = await screen.findByRole("group", { name: "Sonarr" });
    const url = within(sonarr).getByLabelText("URL");
    await userEvent.clear(url);
    await userEvent.type(url, "http://192.0.2.1:8989");
    await userEvent.click(screen.getByRole("button", { name: "Save connections" }));
    await waitFor(() => expect(put.calls).toHaveLength(1));
    expect(bodyOf(put.calls)).toEqual({ connections: { sonarr: { url: "http://192.0.2.1:8989" } } });
    expect(await screen.findByText("Saved. Scan again to apply the changes to the lists.")).toBeInTheDocument();
  });

  it("never renders a secret, shows its source and does not send an empty secret input", async () => {
    const put = recorder(() => ({ status: 200, body: settingsFixture() }));
    mockApi({ [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderWithProviders(<SettingsPage />);
    const sonarr = await screen.findByRole("group", { name: "Sonarr" });
    const key = within(sonarr).getByLabelText("API key") as HTMLInputElement;
    expect(key.value).toBe("");
    expect(key).toHaveAttribute("type", "password");
    expect(key).toHaveAttribute("placeholder", "Enter a new value to replace it");
    expect(within(sonarr).getByText("Set")).toBeInTheDocument();
    expect(within(sonarr).getByText("From the environment")).toBeInTheDocument();
    const qb = screen.getByRole("group", { name: "qBittorrent" });
    expect(within(qb).getByText("Not set")).toBeInTheDocument();
    // Edit the URL, type into the key and empty it again: only the URL goes out.
    const url = within(sonarr).getByLabelText("URL");
    await userEvent.type(url, "9");
    await userEvent.type(key, "abc");
    await userEvent.clear(key);
    await userEvent.click(screen.getByRole("button", { name: "Save connections" }));
    await waitFor(() => expect(put.calls).toHaveLength(1));
    expect(bodyOf(put.calls)).toEqual({ connections: { sonarr: { url: "http://192.0.2.1:89889" } } });
  });

  it("clears a saved API key with null", async () => {
    const put = recorder(() => ({ status: 200, body: settingsFixture() }));
    mockApi({ [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderWithProviders(<SettingsPage />);
    const radarr = await screen.findByRole("group", { name: "Radarr" });
    // The key from the environment has no clear button; the saved one has.
    const sonarr = screen.getByRole("group", { name: "Sonarr" });
    expect(within(sonarr).queryByRole("button", { name: "Clear saved value for API key" })).not.toBeInTheDocument();
    await userEvent.click(within(radarr).getByRole("button", { name: "Clear saved value for API key" }));
    await userEvent.click(screen.getByRole("button", { name: "Save connections" }));
    await waitFor(() => expect(put.calls).toHaveLength(1));
    expect(bodyOf(put.calls)).toEqual({ connections: { radarr: { apiKey: null } } });
  });

  it("disables Test with a hint after an edit, enables it after the save and shows the answer", async () => {
    mockApi({
      [SETTINGS]: ok(settingsFixture()),
      [PUT]: ok(settingsFixture()),
      [USERS]: ok([]),
      "POST /api/connections/sonarr/test": ok({ ok: true, detail: "Sonarr 4.0.12" }),
    });
    renderWithProviders(<SettingsPage />);
    const sonarr = await screen.findByRole("group", { name: "Sonarr" });
    const test = within(sonarr).getByRole("button", { name: "Test" });
    expect(test).toBeEnabled();
    await userEvent.type(within(sonarr).getByLabelText("URL"), "1");
    expect(test).toBeDisabled();
    expect(within(sonarr).getByText("Save to test")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Save connections" }));
    await waitFor(() => expect(within(sonarr).getByRole("button", { name: "Test" })).toBeEnabled());
    await userEvent.click(within(sonarr).getByRole("button", { name: "Test" }));
    expect(await within(sonarr).findByText("Connected: Sonarr 4.0.12")).toBeInTheDocument();
  });

  it("shows the error a failed test answers", async () => {
    mockApi({
      [SETTINGS]: ok(settingsFixture()),
      [USERS]: ok([]),
      "POST /api/connections/radarr/test": ok({ ok: false, error: "Radarr refused the API key." }),
    });
    renderWithProviders(<SettingsPage />);
    const radarr = await screen.findByRole("group", { name: "Radarr" });
    await userEvent.click(within(radarr).getByRole("button", { name: "Test" }));
    expect(await within(radarr).findByText("Radarr refused the API key.")).toBeInTheDocument();
  });

  it("shows a refused connection field next to it", async () => {
    mockApi({
      [SETTINGS]: ok(settingsFixture()),
      [USERS]: ok([]),
      [PUT]: () => ({ status: 400, body: { error: "must be an http or https address", field: "connections.sonarr.url" } }),
    });
    renderWithProviders(<SettingsPage />);
    const sonarr = await screen.findByRole("group", { name: "Sonarr" });
    await userEvent.type(within(sonarr).getByLabelText("URL"), "x");
    await userEvent.click(screen.getByRole("button", { name: "Save connections" }));
    expect(await within(sonarr).findByText("must be an http or https address")).toBeInTheDocument();
  });

  it("shows dry run read-only", async () => {
    mockApi({ [SETTINGS]: ok(settingsFixture()), [USERS]: ok([]) });
    renderWithProviders(<SettingsPage />);
    expect(await screen.findByText("Dry run is on: set by REMOVARR_DRY_RUN.")).toBeInTheDocument();
  });
});

describe("SettingsPage rules", () => {
  it("shows a refused rule next to its field", async () => {
    mockApi({
      [SETTINGS]: ok(settingsFixture()),
      [USERS]: ok([]),
      [PUT]: () => ({ status: 400, body: { error: "must be at least 0", field: "rules.graceDays" } }),
    });
    renderWithProviders(<SettingsPage />);
    const rules = await screen.findByRole("region", { name: "Rules" });
    const grace = within(rules).getByLabelText("Grace period (days)");
    await userEvent.clear(grace);
    await userEvent.type(grace, "-1");
    await userEvent.click(screen.getByRole("button", { name: "Save rules" }));
    const msg = await within(rules).findByText("must be at least 0");
    expect(msg.closest("[data-field='rules.graceDays']")).not.toBeNull();
  });

  it("sends only the changed rule", async () => {
    const put = recorder(() => ({ status: 200, body: settingsFixture() }));
    mockApi({ [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderWithProviders(<SettingsPage />);
    const rules = await screen.findByRole("region", { name: "Rules" });
    const grace = within(rules).getByLabelText("Grace period (days)");
    await userEvent.clear(grace);
    await userEvent.type(grace, "7");
    await userEvent.click(screen.getByRole("button", { name: "Save rules" }));
    await waitFor(() => expect(put.calls).toHaveLength(1));
    expect(bodyOf(put.calls)).toEqual({ rules: { graceDays: 7 } });
  });

  it("splits, trims and drops empty entries of the excluded lists", async () => {
    const put = recorder(() => ({ status: 200, body: settingsFixture() }));
    mockApi({ [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderWithProviders(<SettingsPage />);
    const rules = await screen.findByRole("region", { name: "Rules" });
    await userEvent.type(within(rules).getByLabelText("Excluded categories"), " tv , ,books");
    await userEvent.click(within(rules).getByLabelText("Protect favourites"));
    await userEvent.click(screen.getByRole("button", { name: "Save rules" }));
    await waitFor(() => expect(put.calls).toHaveLength(1));
    expect(bodyOf(put.calls)).toEqual({ rules: { excludedCategories: ["tv", "books"], protectFavorites: false } });
  });
});

describe("SettingsPage counting users", () => {
  it("lists the users, notes an empty selection and saves the selected id", async () => {
    const put = recorder(() => ({ status: 200, body: settingsFixture() }));
    mockApi({
      [SETTINGS]: ok(settingsFixture()),
      [PUT]: put.route,
      [USERS]: ok([{ id: "u1", name: "Ada" }, { id: "u2", name: "Bo" }]),
    });
    renderWithProviders(<SettingsPage />);
    const users = await screen.findByRole("region", { name: "Counting users" });
    expect(await within(users).findByRole("checkbox", { name: "Ada" })).toBeInTheDocument();
    expect(within(users).getByText("Watched lists are not produced until at least one user is selected.")).toBeInTheDocument();
    await userEvent.click(within(users).getByRole("checkbox", { name: "Bo" }));
    await userEvent.click(screen.getByRole("button", { name: "Save counting users" }));
    await waitFor(() => expect(put.calls).toHaveLength(1));
    expect(bodyOf(put.calls)).toEqual({ rules: { countingUsers: ["u2"] } });
  });

  it("asks to connect Jellyfin when the server answers 409", async () => {
    mockApi({
      [SETTINGS]: ok(settingsFixture()),
      [USERS]: () => ({ status: 409, body: { error: "Jellyfin is not configured." } }),
    });
    renderWithProviders(<SettingsPage />);
    const users = await screen.findByRole("region", { name: "Counting users" });
    expect(await within(users).findByText("Connect Jellyfin first.")).toBeInTheDocument();
  });
});
