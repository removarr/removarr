import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Action, ActionsPage } from "@/api/types";
import { formatDateTime } from "@/lib/format";
import { HistoryPage } from "@/pages/HistoryPage";
import { executeFixture } from "@/test/fixtures";
import { mockApi, renderWithProviders } from "@/test/render";

const FIRST = "GET /api/actions?limit=50";
const SECOND = "GET /api/actions?limit=50&before=2";
const [a1, a2] = executeFixture.actions;
const older: Action = {
  id: 1, planId: "plan-0", at: "2026-02-01T08:00:00Z", dryRun: true, outcome: "dry-run", size: 1024 ** 3,
  rows: [{ list: "movies", key: "tmdb:5", title: "Older Movie", size: 1024 ** 3 }],
  steps: [{ manager: { manager: "movies", action: "remove-movie", rowKey: "tmdb:5", itemId: 5 }, result: "dry-run" }],
};
const ok = (body: ActionsPage) => () => ({ status: 200, body });

describe("HistoryPage", () => {
  it("shows each record with time, rows, sizes, outcome and a failed step's error", async () => {
    mockApi({ [FIRST]: ok({ actions: [a2, a1] }) });
    renderWithProviders(<HistoryPage />);
    const items = await screen.findAllByRole("listitem", { name: /^Deletion / });
    expect(items).toHaveLength(2);
    const first = within(items[0]);
    expect(first.getByText(formatDateTime(a2.at))).toBeInTheDocument();
    expect(first.getByText("Show, season 2")).toBeInTheDocument();
    expect(first.getByText("Failed")).toBeInTheDocument();
    expect(first.getByText(/Sonarr answered 500/)).toBeInTheDocument();
    expect(first.getByText(/size 5\.0 GiB/)).toBeInTheDocument();
    const second = within(items[1]);
    expect(second.getByText("Alpha")).toBeInTheDocument();
    expect(second.getByText("(added)")).toBeInTheDocument();
    expect(second.getByText("Done")).toBeInTheDocument();
    expect(second.getByText(/size 7\.0 GiB/)).toBeInTheDocument();
    expect(second.getByText(/kept its data/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("loads the next page with before=<next> and appends it", async () => {
    const api = mockApi({ [FIRST]: ok({ actions: [a2], next: 2 }), [SECOND]: ok({ actions: [older] }) });
    const user = userEvent.setup();
    renderWithProviders(<HistoryPage />);
    await screen.findByText("Show, season 2");
    await user.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByText("Older Movie")).toBeInTheDocument();
    expect(screen.getByText("Show, season 2")).toBeInTheDocument();
    expect(screen.getByText("Dry run")).toBeInTheDocument();
    expect(api.calls).toContain(SECOND);
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("says so when nothing was deleted", async () => {
    mockApi({ [FIRST]: ok({ actions: [] }) });
    renderWithProviders(<HistoryPage />);
    expect(await screen.findByText("Nothing was deleted yet.")).toBeInTheDocument();
  });
});
