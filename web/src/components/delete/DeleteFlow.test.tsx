import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { CandidatesResponse, Plan, Status } from "@/api/types";
import { formatDateTime } from "@/lib/format";
import { ListPage } from "@/pages/ListPage";
import { executeFixture, movieFixture, planFixture, statusFixture } from "@/test/fixtures";
import { mockApi, renderWithProviders, type MockResponse, type MockRoutes } from "@/test/render";

const GiB = 1024 ** 3;
const LIVE = "GET /api/candidates?list=movies&ignored=false";
const STATUS = "GET /api/status";
const PLANS = "POST /api/plans";
const EXEC = "POST /api/plans/plan-1/execute";
const EXEC2 = "POST /api/plans/plan-2/execute";
const ROWS = { rows: [{ list: "movies", key: "tmdb:1" }, { list: "movies", key: "tmdb:2" }] };

const live: CandidatesResponse = {
  list: "movies",
  scannedAt: "2026-03-01T09:00:00Z",
  candidates: [movieFixture("tmdb:1", "Alpha", 4 * GiB), movieFixture("tmdb:2", "Gamma", GiB), movieFixture("tmdb:9", "Zeta", GiB)],
};

const ok = (body: unknown) => () => ({ status: 200, body });
const created = (body: unknown) => () => ({ status: 201, body });

function deferred() {
  let resolve!: (r: MockResponse) => void;
  const promise = new Promise<MockResponse>((r) => (resolve = r));
  return { promise, resolve };
}

// openDelete shows the movie list, selects Alpha and Gamma (not Zeta) and presses Delete.
async function openDelete(routes: MockRoutes, status: Partial<Status> = {}) {
  const api = mockApi({ [STATUS]: ok({ ...statusFixture, ...status }), [LIVE]: ok(live), ...routes });
  const user = userEvent.setup();
  const rendered = renderWithProviders(<ListPage list="movies" />);
  await user.click(await screen.findByRole("checkbox", { name: "Select Alpha" }));
  await user.click(screen.getByRole("checkbox", { name: "Select Gamma" }));
  await user.click(within(screen.getByRole("region", { name: "Selected rows" })).getByRole("button", { name: "Delete" }));
  return { api, user, ...rendered };
}

const dialogButtons = () => within(screen.getByRole("dialog")).queryAllByRole("button").map((b) => b.textContent);
const itemOf = (text: string) => within(screen.getByRole("dialog")).getByText(text).closest("li") as HTMLElement;

describe("delete flow", () => {
  it("posts exactly the selected rows and shows the plan once it is built", async () => {
    const answer = deferred();
    let body: unknown;
    let headers: Record<string, string> = {};
    const { api } = await openDelete({
      [PLANS]: (init) => {
        body = JSON.parse(String(init.body));
        headers = init.headers as Record<string, string>;
        return answer.promise;
      },
    });
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText("Building the plan… This reads every app and takes about as long as a scan."),
    ).toBeInTheDocument();
    expect(body).toEqual(ROWS);
    expect(headers["X-Removarr-Request"]).toBe("1");
    expect(dialogButtons()).toEqual(["Cancel"]);
    answer.resolve({ status: 201, body: planFixture });
    expect(await within(dialog).findByText("3 items · 13.0 GiB")).toBeInTheDocument();
    expect(api.count(PLANS)).toBe(1);
  });

  // Building a plan changes nothing, so it can be abandoned; the plan that comes back later
  // must not reopen the dialog.
  it("closes on Cancel while the plan is built, keeps the selection and ignores the late plan", async () => {
    const answer = deferred();
    const { api, user } = await openDelete({ [PLANS]: () => answer.promise });
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByRole("checkbox", { name: "Select Alpha" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Select Gamma" })).toBeChecked();
    expect(screen.getByRole("region", { name: "Selected rows" })).toBeInTheDocument();

    answer.resolve({ status: 201, body: planFixture });
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("3 items · 13.0 GiB")).not.toBeInTheDocument();
    expect(api.count(PLANS)).toBe(1);
  });

  it("closes on Escape while the plan is built", async () => {
    const answer = deferred();
    const { user } = await openDelete({ [PLANS]: () => answer.promise });
    await screen.findByRole("dialog");
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    answer.resolve({ status: 201, body: planFixture });
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("shows the plan grouped by app with every note, the kept torrents and the skipped rows", async () => {
    await openDelete({ [PLANS]: created(planFixture) });
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText("3 items · 13.0 GiB");
    expect(within(dialog).getByText(`Valid until ${formatDateTime(planFixture.expiresAt)}`)).toBeInTheDocument();
    expect(within(dialog).queryByText(/^Dry run:/)).not.toBeInTheDocument();

    const radarr = within(dialog).getByRole("region", { name: "Radarr" });
    expect(within(radarr).getByText("Alpha")).toBeInTheDocument();
    expect(within(itemOf("Beta")).getByText("Added: deleted together with Alpha, through Alpha.Beta.Pack")).toBeInTheDocument();
    expect(within(itemOf("Delta")).getByText("Added: deleted together with Alpha, through a shared library file")).toBeInTheDocument();
    expect(within(itemOf("Delta")).getByText("Not in the latest scan")).toBeInTheDocument();
    expect(within(itemOf("Alpha")).getByText("4.0 GiB")).toBeInTheDocument();
    expect(
      within(radarr).getByText("Remove Alpha from Radarr with its files, and add it to the import exclusion list"),
    ).toBeInTheDocument();
    expect(within(radarr).getByText("Remove Beta from Radarr with its files")).toBeInTheDocument();

    const sonarr = within(dialog).getByRole("region", { name: "Sonarr" });
    expect(within(sonarr).getByText("Show, season 2")).toBeInTheDocument();
    expect(within(sonarr).getByText("Unmonitor 3 episodes of Show, season 2")).toBeInTheDocument();
    expect(within(sonarr).getByText("Unmonitor season 2 (Show, season 2)")).toBeInTheDocument();
    expect(within(sonarr).getByText("Delete 3 episode files of Show, season 2")).toBeInTheDocument();

    const qbit = within(dialog).getByRole("region", { name: "qBittorrent" });
    // A removed torrent can keep its data, so the heading claims nothing about the data.
    expect(within(qbit).getByRole("heading", { name: "Removed" })).toBeInTheDocument();
    expect(within(qbit).queryByText(/with their data/)).not.toBeInTheDocument();
    const pack = within(qbit).getByText("Alpha.Beta.Pack").closest("li") as HTMLElement;
    expect(within(pack).getByText("12.0 GiB")).toBeInTheDocument();
    expect(within(pack).getByText("Below seeding minimum")).toBeInTheDocument();
    expect(within(pack).getByText("Outside the library: 6.0 GiB")).toBeInTheDocument();
    expect(within(pack).queryByText(/Keeps its data/)).not.toBeInTheDocument();
    const remux = within(qbit).getByText("Alpha.Remux").closest("li") as HTMLElement;
    expect(within(remux).getByText("Keeps its data: shared with another download, or its path is unknown")).toBeInTheDocument();
    expect(within(remux).getByText("Linked by file size only: check that it belongs")).toBeInTheDocument();
    expect(within(qbit).getByText("Orphan.Torrent")).toBeInTheDocument();
    expect(within(within(qbit).getByText("Alpha.Queued").closest("li") as HTMLElement).getByText("In a download queue")).toBeInTheDocument();
    expect(
      within(within(qbit).getByText("Show.S02.Pack").closest("li") as HTMLElement).getByText("Still backs a library file that stays"),
    ).toBeInTheDocument();

    const skipped = within(dialog).getByRole("region", { name: "Skipped" });
    expect(within(skipped).getByText("Gamma: It no longer qualifies.")).toBeInTheDocument();

    expect(dialogButtons()).toEqual(["Cancel", "Delete"]);
  });

  it("says that a dry run changes nothing and offers to run it as one", async () => {
    await openDelete({ [PLANS]: created(planFixture) }, { dryRun: true });
    const dialog = await screen.findByRole("dialog");
    expect(
      await within(dialog).findByText("Dry run: nothing will be changed. The history records what would have run."),
    ).toBeInTheDocument();
    expect(dialogButtons()).toEqual(["Cancel", "Run as dry run"]);
  });

  it("executes the plan once on confirmation, shows the outcome and refreshes the lists on close", async () => {
    const answer = deferred();
    let body: unknown;
    const { api, user, queryClient } = await openDelete({
      [PLANS]: created(planFixture),
      [EXEC]: (init) => {
        body = JSON.parse(String(init.body));
        return answer.promise;
      },
    });
    queryClient.setQueryData(["actions"], { actions: [] });
    const dialog = await screen.findByRole("dialog");
    await user.click(await within(dialog).findByRole("button", { name: "Delete" }));
    expect(
      within(dialog).getByText("Deleting… This continues on the server if you close the page; the History page shows the result."),
    ).toBeInTheDocument();
    expect(dialogButtons()).toEqual([]);
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    answer.resolve({ status: 200, body: executeFixture });

    expect(await within(dialog).findByText("1 done · 1 failed")).toBeInTheDocument();
    expect(body).toEqual({});
    expect(api.count(EXEC)).toBe(1);
    expect(within(dialog).getByText("Done")).toBeInTheDocument();
    expect(within(dialog).getByText("Failed")).toBeInTheDocument();
    expect(within(dialog).getByText("failed: Sonarr answered 500.")).toBeInTheDocument();
    expect(within(itemOf("Alpha.Remux")).getByText("kept its data")).toBeInTheDocument();
    expect(within(itemOf("Alpha.Remux")).getByText("already gone")).toBeInTheDocument();
    expect(within(itemOf("Show.S02.Extra")).getByText("skipped")).toBeInTheDocument();
    expect(within(itemOf("Delete 3 episode files of Show, season 2")).getByText("failed: Sonarr answered 500.")).toBeInTheDocument();

    const lists = api.count(LIVE);
    const statuses = api.count(STATUS);
    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(api.count(LIVE)).toBe(lists + 1));
    await waitFor(() => expect(api.count(STATUS)).toBe(statuses + 1));
    expect(queryClient.getQueryState(["actions"])?.isInvalidated).toBe(true);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Selected rows" })).not.toBeInTheDocument();
    expect(api.count(EXEC)).toBe(1);
  });

  it("builds one plan when Delete is double-clicked", async () => {
    const answer = deferred();
    const api = mockApi({ [STATUS]: ok(statusFixture), [LIVE]: ok(live), [PLANS]: () => answer.promise });
    const user = userEvent.setup();
    renderWithProviders(<ListPage list="movies" />);
    await user.click(await screen.findByRole("checkbox", { name: "Select Alpha" }));
    const del = within(screen.getByRole("region", { name: "Selected rows" })).getByRole("button", { name: "Delete" });
    fireEvent.click(del);
    fireEvent.click(del);
    await screen.findByRole("dialog");
    answer.resolve({ status: 201, body: planFixture });
    await within(screen.getByRole("dialog")).findByText("3 items · 13.0 GiB");
    expect(api.count(PLANS)).toBe(1);
  });

  it("refreshes the lists when the page goes away during the execution", async () => {
    const answer = deferred();
    const { user, queryClient, unmount } = await openDelete({ [PLANS]: created(planFixture), [EXEC]: () => answer.promise });
    queryClient.setQueryData(["actions"], { actions: [] });
    const dialog = await screen.findByRole("dialog");
    await user.click(await within(dialog).findByRole("button", { name: "Delete" }));
    unmount();
    answer.resolve({ status: 200, body: executeFixture });
    await waitFor(() => expect(queryClient.getQueryState(["candidates", "movies", false])?.isInvalidated).toBe(true));
    expect(queryClient.getQueryState(["status"])?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(["actions"])?.isInvalidated).toBe(true);
  });

  it("shows the running job when executing is refused, says nothing ran and does not retry", async () => {
    const { api } = await openDelete({
      [PLANS]: created(planFixture),
      [EXEC]: () => ({ status: 409, body: { error: "A scan is running." } }),
    });
    const dialog = await screen.findByRole("dialog");
    await userEvent.setup().click(await within(dialog).findByRole("button", { name: "Delete" }));
    expect(await within(dialog).findByText("A scan is running.")).toBeInTheDocument();
    expect(within(dialog).queryByText(/may have run in part/)).not.toBeInTheDocument();
    expect(dialogButtons()).toEqual(["Close"]);
    expect(api.count(EXEC)).toBe(1);
  });

  it.each([
    [410, "The plan has expired. Build it again."],
    [404, "There is no such plan. It was executed or discarded already: build it again."],
  ])("after a %i on execute builds the same rows again and waits for a new confirmation", async (code, message) => {
    const plan2: Plan = { ...planFixture, id: "plan-2", expiresAt: "2026-03-01T11:10:00Z" };
    const bodies: unknown[] = [];
    const { api, user } = await openDelete({
      [PLANS]: (init) => {
        bodies.push(JSON.parse(String(init.body)));
        return { status: 201, body: bodies.length === 1 ? planFixture : plan2 };
      },
      [EXEC]: () => ({ status: code, body: { error: message } }),
      [EXEC2]: ok({ ...executeFixture, planId: "plan-2" }),
    });
    const dialog = await screen.findByRole("dialog");
    await user.click(await within(dialog).findByRole("button", { name: "Delete" }));
    expect(await within(dialog).findByText(message)).toBeInTheDocument();
    expect(dialogButtons()).toContain("Build the plan again");
    expect(dialogButtons()).not.toContain("Delete");

    await user.click(within(dialog).getByRole("button", { name: "Build the plan again" }));
    expect(await within(dialog).findByText(`Valid until ${formatDateTime(plan2.expiresAt)}`)).toBeInTheDocument();
    expect(bodies).toEqual([ROWS, ROWS]);
    expect(api.count(EXEC)).toBe(1);
    expect(api.count(EXEC2)).toBe(0);
    expect(dialogButtons()).toEqual(["Cancel", "Delete"]);

    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    expect(await within(dialog).findByText("1 done · 1 failed")).toBeInTheDocument();
    expect(api.count(EXEC2)).toBe(1);
    expect(api.count(EXEC)).toBe(1);
  });

  it("shows the running job when building is refused and offers only Close", async () => {
    const { api, user } = await openDelete({ [PLANS]: () => ({ status: 409, body: { error: "A scan is running." } }) });
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText(/a scan is running/i)).toBeInTheDocument();
    expect(dialogButtons()).toEqual(["Close"]);
    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(api.count(PLANS)).toBe(1);
    expect(screen.getByRole("region", { name: "Selected rows" })).toBeInTheDocument();
  });

  it("says when nothing can be deleted and offers no Delete", async () => {
    await openDelete({ [PLANS]: created({ ...planFixture, items: [] }) });
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText("Nothing can be deleted.")).toBeInTheDocument();
    expect(within(dialog).getByText("Gamma: It no longer qualifies.")).toBeInTheDocument();
    expect(dialogButtons()).toEqual(["Close"]);
  });
});
