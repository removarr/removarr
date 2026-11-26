import { expect, test, type Page } from "@playwright/test";

// The whole UI against removarr's real API on fake apps (internal/e2e/fakeserver): a scan,
// an ignore, a deletion, its history and the settings. The fake library is described in
// internal/e2e/fakeserver/fakes.go; dry run is off, so the deletion changes it.

// rows are the row checkboxes of the shown list, without the one that selects all.
const rows = (page: Page) => page.getByRole("checkbox", { name: /^Select (?!all shown rows)/ });

const nav = (page: Page, name: string) => page.getByRole("navigation", { name: "Main" }).getByRole("link", { name });

// shownPanelLinks are the side panel's links a person can see. A closed panel is hidden from
// the accessibility tree at once, so it is found by CSS; it is out of sight only once its
// closing animation ended, which toHaveCount waits for.
const shownPanelLinks = (page: Page) => page.locator('nav[aria-label="Main"] a:visible');

// A link of the closed panel cannot take the focus.
const panelLinkTakesFocus = (page: Page) =>
  page.evaluate(`(() => {
    const a = document.querySelector('nav[aria-label="Main"] a');
    a.focus();
    return document.activeElement === a;
  })()`);

// scanAtRightEnd checks that the Scan button is the right-most thing in the header, past only
// the header's padding, and returns its box.
async function scanAtRightEnd(page: Page) {
  const header = (await page.locator("header").boundingBox())!;
  const scan = (await page.getByRole("button", { name: "Scan", exact: true }).boundingBox())!;
  expect(header.x + header.width - (scan.x + scan.width)).toBeLessThanOrEqual(24);
  return scan;
}

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
    // On a wide screen the scan's details sit between the name and the Scan button.
    const scan = await scanAtRightEnd(page);
    const last = (await page.getByText(/^Last scan /).boundingBox())!;
    expect(last.x + last.width).toBeLessThanOrEqual(scan.x);
    expect(last.y).toBeLessThan(scan.y + scan.height);
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

    await expect(bar).toHaveCount(0);
    await page.getByRole("checkbox", { name: "Select Fake Movie Two" }).check();
    // The bar is fixed to the bottom of the window, beside the side panel and not over it,
    // and the list is padded so that its last row stays above the bar.
    await expect(bar).toHaveCSS("position", "fixed");
    const panel = await page.getByRole("navigation", { name: "Main" }).boundingBox();
    const barBox = await bar.boundingBox();
    expect(barBox!.x).toBeGreaterThanOrEqual(panel!.x + panel!.width - 1);
    expect(barBox!.y + barBox!.height).toBeCloseTo(page.viewportSize()!.height, 0);
    // In a window this low the last row would sit under the bar without the list's padding.
    const size = page.viewportSize()!;
    await page.setViewportSize({ width: size.width, height: 300 });
    await page.evaluate("window.scrollTo(0, document.documentElement.scrollHeight)");
    const last = await page.getByRole("row").last().boundingBox();
    expect(last!.y + last!.height).toBeLessThanOrEqual((await bar.boundingBox())!.y);
    await page.setViewportSize(size);
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
    await expect(page).toHaveURL(/\/settings\/connections$/);
    await expect(page.getByRole("heading", { level: 1, name: "Connections" })).toBeVisible();
    await expect(nav(page, "Connections")).toHaveAttribute("aria-current", "page");
    const jellyfin = page.getByRole("group", { name: "Jellyfin" });
    await expect(jellyfin.getByText("From the environment").first()).toBeVisible();
    await jellyfin.getByRole("button", { name: "Test" }).click();
    await expect(jellyfin.getByRole("status")).toHaveText("Connected: fake");
  });

  await test.step("the counting users and the rules are pages of their own", async () => {
    await nav(page, "Counting users").click();
    await expect(page).toHaveURL(/\/settings\/counting-users$/);
    await expect(page.getByRole("heading", { level: 1, name: "Counting users" })).toBeVisible();
    await expect(page.getByRole("region", { name: "Counting users" })).toBeVisible();
    await expect(page.getByRole("group", { name: "Jellyfin" })).toHaveCount(0);

    await nav(page, "Rules").click();
    await expect(page).toHaveURL(/\/settings\/rules$/);
    await expect(page.getByRole("heading", { level: 1, name: "Rules" })).toBeVisible();
    await expect(page.getByLabel("Grace period (days)")).toBeVisible();
    await expect(nav(page, "Settings")).toHaveAttribute("aria-current", "true");
  });

  await test.step("the side panel hides, and stays hidden after a reload", async () => {
    await page.getByRole("button", { name: "Hide menu" }).click();
    await expect(page.getByRole("navigation", { name: "Main" })).toBeHidden();
    await expect(shownPanelLinks(page)).toHaveCount(0);
    expect(await panelLinkTakesFocus(page)).toBe(false);
    // Closed, the panel takes no width at all, not even its border.
    expect(await page.evaluate(`document.querySelector('nav[aria-label="Main"]').getBoundingClientRect().width`)).toBe(0);
    await page.reload();
    await expect(page.getByRole("heading", { level: 1, name: "Rules" })).toBeVisible();
    await expect(page.getByRole("navigation", { name: "Main" })).toBeHidden();
    await page.getByRole("button", { name: "Show menu" }).click();
    await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  });

  await test.step("on a narrow screen the menu is a drawer that closes when a page is chosen", async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    const menu = page.getByRole("navigation", { name: "Main" });
    await expect(menu).toBeHidden();
    await expect(shownPanelLinks(page)).toHaveCount(0);
    await page.getByRole("button", { name: "Show menu" }).click();
    await expect(menu).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(menu).toBeHidden();
    await expect(shownPanelLinks(page)).toHaveCount(0);
    expect(await panelLinkTakesFocus(page)).toBe(false);
    await page.getByRole("button", { name: "Show menu" }).click();
    await nav(page, "Movies").click();
    await expect(page.getByRole("heading", { level: 1, name: "Movies" })).toBeVisible();
    await expect(menu).toBeHidden();

    // On a narrow screen Scan stays at the right end and the scan's details take the row below.
    const scan = await scanAtRightEnd(page);
    const last = (await page.getByText(/^Last scan /).boundingBox())!;
    expect(last.y).toBeGreaterThanOrEqual(scan.y + scan.height);

    // The details collapse to a one-line summary, with no warning for the fake library, and
    // come back.
    const details = page.getByTestId("scan-details");
    const open = (await details.boundingBox())!.height;
    await page.getByRole("button", { name: "Hide scan details" }).click();
    await expect(page.getByTestId("scan-summary")).toHaveText(/^Last scan /);
    await expect(page.getByTestId("attention")).toHaveCount(0);
    await expect.poll(async () => (await details.boundingBox())!.height).toBeLessThan(open);
    await expect.poll(async () => (await details.boundingBox())!.height).toBeLessThanOrEqual(48);
    await page.getByRole("button", { name: "Show scan details" }).click();
    await expect(page.getByTestId("scan-summary")).toHaveCount(0);
    await expect(page.getByTestId("counts")).toBeVisible();
    await expect.poll(async () => (await details.boundingBox())!.height).toBeCloseTo(open, 0);

    // On a narrow screen the bar spans the whole width.
    await page.getByRole("checkbox", { name: "Select Fake Movie Two" }).check();
    const bar = await page.getByRole("region", { name: "Selected rows" }).boundingBox();
    expect(bar!.x).toBe(0);
    expect(bar!.width).toBeCloseTo(390, 0);
  });
});
