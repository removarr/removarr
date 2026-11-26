import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Settings } from "@/api/types";
import { mockApi, renderApp } from "@/test/render";

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

// Each section is a page of its own; the shell around it reads the status.
const shell = {
  "GET /api/status": ok({ dryRun: true, connections: {}, scan: { running: false }, lastScan: null, stale: false }),
};
const bodyOf = (calls: { init: RequestInit }[]) => JSON.parse(String(calls[0].init.body));

// A route that records the body of each request.
function recorder(answer: (body: unknown) => { status: number; body?: unknown }) {
  const calls: { init: RequestInit }[] = [];
  return { calls, route: (init: RequestInit) => { calls.push({ init }); return answer(JSON.parse(String(init.body))); } };
}

describe("SettingsPage connections", () => {
  it("saving a changed URL sends that URL only", async () => {
    const put = recorder(() => ({ status: 200, body: settingsFixture() }));
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderApp("/settings/connections");
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
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderApp("/settings/connections");
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
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderApp("/settings/connections");
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
    mockApi({ ...shell,
      [SETTINGS]: ok(settingsFixture()),
      [PUT]: ok(settingsFixture()),
      [USERS]: ok([]),
      "POST /api/connections/sonarr/test": ok({ ok: true, detail: "Sonarr 4.0.12" }),
    });
    renderApp("/settings/connections");
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
    mockApi({ ...shell,
      [SETTINGS]: ok(settingsFixture()),
      [USERS]: ok([]),
      "POST /api/connections/radarr/test": ok({ ok: false, error: "Radarr refused the API key." }),
    });
    renderApp("/settings/connections");
    const radarr = await screen.findByRole("group", { name: "Radarr" });
    await userEvent.click(within(radarr).getByRole("button", { name: "Test" }));
    expect(await within(radarr).findByText("Radarr refused the API key.")).toBeInTheDocument();
  });

  it("shows a refused connection field next to it", async () => {
    mockApi({ ...shell,
      [SETTINGS]: ok(settingsFixture()),
      [USERS]: ok([]),
      [PUT]: () => ({ status: 400, body: { error: "must be an http or https address", field: "connections.sonarr.url" } }),
    });
    renderApp("/settings/connections");
    const sonarr = await screen.findByRole("group", { name: "Sonarr" });
    await userEvent.type(within(sonarr).getByLabelText("URL"), "x");
    await userEvent.click(screen.getByRole("button", { name: "Save connections" }));
    expect(await within(sonarr).findByText("must be an http or https address")).toBeInTheDocument();
  });

  it("keeps browser autofill out: secrets ask for a new password, the user name has its own name", async () => {
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [USERS]: ok([]) });
    renderApp("/settings/connections");
    const qb = await screen.findByRole("group", { name: "qBittorrent" });
    expect(within(qb).getByLabelText("Password")).toHaveAttribute("autocomplete", "new-password");
    expect(within(qb).getByLabelText("User name")).toHaveAttribute("autocomplete", "off");
    expect(within(qb).getByLabelText("User name")).toHaveAttribute("name", "removarr-qbittorrent-username");
    for (const app of ["Jellyfin", "Sonarr", "Radarr"]) {
      const g = screen.getByRole("group", { name: app });
      expect(within(g).getByLabelText("API key")).toHaveAttribute("autocomplete", "new-password");
    }
  });

  it("does not count an edit equal to the shown value as a change", async () => {
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [USERS]: ok([]) });
    renderApp("/settings/connections");
    const qb = await screen.findByRole("group", { name: "qBittorrent" });
    await userEvent.type(within(qb).getByLabelText("URL"), "x");
    expect(within(qb).getByRole("button", { name: "Test" })).toBeDisabled();
    await userEvent.type(within(qb).getByLabelText("URL"), "{Backspace}");
    expect(within(qb).getByRole("button", { name: "Test" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Save connections" })).toBeDisabled();
  });

  it("hints that an emptied field is not saved, and does not send it", async () => {
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [USERS]: ok([]) });
    renderApp("/settings/connections");
    const qb = await screen.findByRole("group", { name: "qBittorrent" });
    expect(within(qb).queryByText(/Empty fields are not saved/)).not.toBeInTheDocument();
    await userEvent.clear(within(qb).getByLabelText("User name"));
    expect(
      within(qb).getByText("Empty fields are not saved. Use Clear saved value to remove a saved value."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save connections" })).toBeDisabled();
  });

  it("drops an old test result after a successful save of that app", async () => {
    mockApi({ ...shell,
      [SETTINGS]: ok(settingsFixture()),
      [PUT]: ok(settingsFixture()),
      [USERS]: ok([]),
      "POST /api/connections/sonarr/test": ok({ ok: true, detail: "Sonarr 4.0.12" }),
    });
    renderApp("/settings/connections");
    const sonarr = await screen.findByRole("group", { name: "Sonarr" });
    await userEvent.click(within(sonarr).getByRole("button", { name: "Test" }));
    expect(await within(sonarr).findByText("Connected: Sonarr 4.0.12")).toBeInTheDocument();
    await userEvent.type(within(sonarr).getByLabelText("URL"), "1");
    await userEvent.click(screen.getByRole("button", { name: "Save connections" }));
    await waitFor(() => expect(within(sonarr).queryByText("Connected: Sonarr 4.0.12")).not.toBeInTheDocument());
  });

  it("shows dry run read-only", async () => {
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [USERS]: ok([]) });
    renderApp("/settings/connections");
    expect(await screen.findByText("Dry run is on: set by REMOVARR_DRY_RUN.")).toBeInTheDocument();
  });
});

describe("SettingsPage rules", () => {
  it("shows a refused rule next to its field", async () => {
    mockApi({ ...shell,
      [SETTINGS]: ok(settingsFixture()),
      [USERS]: ok([]),
      [PUT]: () => ({ status: 400, body: { error: "must be at least 0", field: "rules.graceDays" } }),
    });
    renderApp("/settings/rules");
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
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderApp("/settings/rules");
    const rules = await screen.findByRole("region", { name: "Rules" });
    const grace = within(rules).getByLabelText("Grace period (days)");
    await userEvent.clear(grace);
    await userEvent.type(grace, "7");
    await userEvent.click(screen.getByRole("button", { name: "Save rules" }));
    await waitFor(() => expect(put.calls).toHaveLength(1));
    expect(bodyOf(put.calls)).toEqual({ rules: { graceDays: 7 } });
  });

  // A save that succeeds drops the edits, so text typed while it is pending would be lost.
  it("disables the section's inputs while its save is pending", async () => {
    let finish!: () => void;
    const answer = new Promise<{ status: number; body: unknown }>((r) => (finish = () => r({ status: 200, body: settingsFixture() })));
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [PUT]: () => answer, [USERS]: ok([]) });
    renderApp("/settings/rules");
    const rules = await screen.findByRole("region", { name: "Rules" });
    const grace = within(rules).getByLabelText("Grace period (days)");
    await userEvent.clear(grace);
    await userEvent.type(grace, "7");
    await userEvent.click(screen.getByRole("button", { name: "Save rules" }));
    await waitFor(() => expect(grace).toBeDisabled());
    expect(within(rules).getByLabelText("Protection tag")).toBeDisabled();
    expect(within(rules).getByLabelText("Watched mode")).toBeDisabled();
    expect(within(rules).getByLabelText("Protect favourites")).toBeDisabled();
    finish();
    await waitFor(() => expect(grace).toBeEnabled());
    expect(await within(rules).findByRole("status")).toHaveTextContent("Saved.");
  });

  it("splits, trims and drops empty entries of the excluded lists", async () => {
    const put = recorder(() => ({ status: 200, body: settingsFixture() }));
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [PUT]: put.route, [USERS]: ok([]) });
    renderApp("/settings/rules");
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
    mockApi({ ...shell,
      [SETTINGS]: ok(settingsFixture()),
      [PUT]: put.route,
      [USERS]: ok([{ id: "u1", name: "Ada" }, { id: "u2", name: "Bo" }]),
    });
    renderApp("/settings/counting-users");
    const users = await screen.findByRole("region", { name: "Counting users" });
    expect(await within(users).findByRole("checkbox", { name: "Ada" })).toBeInTheDocument();
    expect(within(users).getByText("Watched lists are not produced until at least one user is selected.")).toBeInTheDocument();
    await userEvent.click(within(users).getByRole("checkbox", { name: "Bo" }));
    await userEvent.click(screen.getByRole("button", { name: "Save counting users" }));
    await waitFor(() => expect(put.calls).toHaveLength(1));
    expect(bodyOf(put.calls)).toEqual({ rules: { countingUsers: ["u2"] } });
  });

  it("asks to connect Jellyfin when the server answers 409", async () => {
    mockApi({ ...shell,
      [SETTINGS]: ok(settingsFixture()),
      [USERS]: () => ({ status: 409, body: { error: "Jellyfin is not configured." } }),
    });
    renderApp("/settings/counting-users");
    const users = await screen.findByRole("region", { name: "Counting users" });
    expect(await within(users).findByText("Connect Jellyfin first.")).toBeInTheDocument();
  });
});

describe("the settings pages", () => {
  it.each([
    ["/settings/connections", "Connections", ["Counting users", "Rules"]],
    ["/settings/counting-users", "Counting users", ["Connections", "Rules"]],
    ["/settings/rules", "Rules", ["Connections", "Counting users"]],
  ])("shows %s with its own heading and no other section", async (path, name, others) => {
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [USERS]: ok([]) });
    renderApp(path);
    expect(await screen.findByRole("heading", { level: 1, name })).toBeInTheDocument();
    expect(await screen.findByRole("region", { name })).toBeInTheDocument();
    for (const other of others) expect(screen.queryByRole("region", { name: other })).not.toBeInTheDocument();
  });

  it("shows the dry run line on the connections page only", async () => {
    mockApi({ ...shell, [SETTINGS]: ok(settingsFixture()), [USERS]: ok([]) });
    const first = renderApp("/settings/rules");
    await screen.findByRole("region", { name: "Rules" });
    expect(screen.queryByText("Dry run is on: set by REMOVARR_DRY_RUN.")).not.toBeInTheDocument();
    first.unmount();
    renderApp("/settings/connections");
    expect(await screen.findByText("Dry run is on: set by REMOVARR_DRY_RUN.")).toBeInTheDocument();
  });
});
