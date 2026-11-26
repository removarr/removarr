import { expect, test, type Page } from "@playwright/test";

// The whole UI against removarr's real API on fake apps (internal/e2e/fakeserver): a scan,
// an ignore, a deletion, its history and the settings. The fake library is described in
// internal/e2e/fakeserver/fakes.go; dry run is off, so the deletion changes it.

// rows are the row checkboxes of the shown list, without the one that selects all.
const rows = (page: Page) => page.getByRole("checkbox", { name: /^Select (?!all shown rows)/ });

const nav = (page: Page, name: string) => page.getByRole("navigation", { name: "Main" }).getByRole("link", { name });

test("scan, ignore, delete, history and settings", async ({ page }) => {
  await test.step("the start page is Movies, with no scan yet", async () => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/movies$/);
    await expect(page.getByRole("heading", { name: "Movies" })).toBeVisible();
    await expect(page.getByText("No scan yet", { exact: true })).toBeVisible();
  });

  await test.step("a scan lists two movies and the orphan torrent", async () => {
    await page.getByRole("button", { name: "Scan", exact: true }).click();
    await expect(page.getByText(/^Last scan /)).toBeVisible();
    await expect(rows(page)).toHaveCount(2);
    await expect(page.getByRole("checkbox", { name: "Select Fake Movie One" })).toBeVisible();
    await expect(page.getByRole("checkbox", { name: "Select Fake Movie Two" })).toBeVisible();

    await nav(page, "Torrents").click();
    await expect(page.getByRole("heading", { name: "Torrents" })).toBeVisible();
    await expect(rows(page)).toHaveCount(1);
    await expect(page.getByRole("checkbox", { name: "Select fake.orphan.torrent" })).toBeVisible();
    await nav(page, "Movies").click();
    await expect(rows(page)).toHaveCount(2);
  });

  await test.step("an ignored movie leaves the list and comes back when un-ignored", async () => {
    const bar = page.getByRole("region", { name: "Selected rows" });
    const showIgnored = page.getByRole("switch", { name: "Show ignored" });

    await page.getByRole("checkbox", { name: "Select Fake Movie Two" }).check();
    await bar.getByRole("button", { name: "Ignore", exact: true }).click();
    await expect(rows(page)).toHaveCount(1);
    await expect(page.getByRole("checkbox", { name: "Select Fake Movie Two" })).toHaveCount(0);

    await showIgnored.click();
    await expect(showIgnored).toBeChecked();
    await expect(rows(page)).toHaveCount(1);
    await page.getByRole("checkbox", { name: "Select Fake Movie Two" }).check();
    await bar.getByRole("button", { name: "Un-ignore" }).click();
    await expect(page.getByText("Nothing is ignored in this list.")).toBeVisible();

    await showIgnored.click();
    await expect(showIgnored).not.toBeChecked();
    await expect(rows(page)).toHaveCount(2);
  });

  await test.step("deleting a movie removes it in Radarr and its torrent in qBittorrent", async () => {
    await page.getByRole("checkbox", { name: "Select Fake Movie One" }).check();
    await page.getByRole("region", { name: "Selected rows" }).getByRole("button", { name: "Delete" }).click();

    const dialog = page.getByRole("dialog", { name: "Confirm the deletion" });
    await expect(dialog).toBeVisible();
    const radarr = dialog.getByRole("region", { name: "Radarr" });
    await expect(radarr.getByText("Remove Fake Movie One from Radarr with its files")).toBeVisible();
    const qbittorrent = dialog.getByRole("region", { name: "qBittorrent" });
    await expect(qbittorrent.getByText("Removed")).toBeVisible();
    await expect(qbittorrent.getByText("Fake.Movie.One.2020.1080p")).toBeVisible();

    await dialog.getByRole("button", { name: "Delete" }).click();
    const outcome = page.getByRole("dialog", { name: "Outcome" });
    await expect(outcome.getByText("1 done · 0 failed")).toBeVisible();
    await outcome.getByRole("button", { name: "Close" }).click();
    await expect(outcome).toHaveCount(0);

    await expect(rows(page)).toHaveCount(1);
    await expect(page.getByRole("checkbox", { name: "Select Fake Movie One" })).toHaveCount(0);
    await expect(page.getByRole("checkbox", { name: "Select Fake Movie Two" })).toBeVisible();
  });

  await test.step("the history records the deletion with its steps done", async () => {
    await nav(page, "History").click();
    await expect(page.getByRole("heading", { name: "History" })).toBeVisible();
    const records = page.getByRole("listitem", { name: /^Deletion / });
    await expect(records).toHaveCount(1);
    const record = records.first();
    await expect(record.getByText("Done", { exact: true })).toBeVisible();
    await expect(record.getByText("Fake Movie One", { exact: true })).toBeVisible();
    await expect(record.getByText("Remove Fake Movie One from Radarr with its files — done")).toBeVisible();
    await expect(record.getByText("Fake.Movie.One.2020.1080p — done")).toBeVisible();
  });

  await test.step("the settings show where Jellyfin's connection comes from, and test it", async () => {
    await nav(page, "Settings").click();
    const jellyfin = page.getByRole("group", { name: "Jellyfin" });
    await expect(jellyfin.getByText("From the environment").first()).toBeVisible();
    await jellyfin.getByRole("button", { name: "Test" }).click();
    await expect(jellyfin.getByRole("status")).toHaveText("Connected: fake");
  });
});
