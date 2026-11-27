import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { Candidate, TorrentInfo } from "@/api/types";
import { CandidateList, type CandidateListProps, type ColumnSpec } from "./CandidateList";

const GiB = 1024 ** 3;

function torrent(over: Partial<TorrentInfo> = {}): TorrentInfo {
  return {
    id: "t", name: "torrent", size: GiB, state: "uploading", category: "", tracker: "",
    ratio: 1, seedingDays: 1, addedAt: "2026-01-01T00:00:00Z", belowSeedMin: false, ...over,
  };
}

function row(key: string, title: string, size: number, over: Partial<Candidate> = {}): Candidate {
  return { list: "movies", key, title, size, torrents: [], belowSeedMin: false, ...over };
}

const alpha = row("movie:1", "Alpha", 3 * GiB, { year: 1999, watchedAt: "2026-03-01T00:00:00Z" });
const beta = row("movie:2", "Beta", 1 * GiB, { year: 2004 });
const gamma = row("movie:3", "Gamma", 2 * GiB, { year: 2011, watchedAt: "2026-01-01T00:00:00Z" });

const columns: ColumnSpec[] = [
  { id: "title", header: "Title", cell: (r) => r.title },
  { id: "size", header: "Size", cell: (r) => String(r.size) },
];

const sortKeys: CandidateListProps["sortKeys"] = [
  { id: "size", label: "Size" },
  { id: "watchedAt", label: "Watched" },
  { id: "title", label: "Title" },
];

// setup renders the list with a "Delete" action that reports the keys it was given.
function setup(rows: Candidate[], over: Partial<CandidateListProps> = {}) {
  const onDelete = vi.fn<(keys: string[]) => void>();
  const props: CandidateListProps = {
    rows,
    columns,
    sortKeys,
    renderCard: (r) => <span>{r.title}</span>,
    actions: (selected) => (
      <button type="button" onClick={() => onDelete(selected.map((r) => r.key))}>Delete</button>
    ),
    emptyText: "Nothing to remove.",
    ...over,
  };
  const user = userEvent.setup();
  const view = render(<CandidateList {...props} />);
  const rerender = (next: Candidate[]) => view.rerender(<CandidateList {...props} rows={next} />);
  return { user, onDelete, rerender };
}

const bar = () => screen.queryByRole("region", { name: "Selected rows" });
const selectAllBox = () => screen.getByRole("checkbox", { name: "Select all shown rows" });
const filterBox = () => screen.getByRole("searchbox", { name: "Filter" });

// shownTitles reads the order of the rows from their checkboxes.
function shownTitles(): string[] {
  return screen
    .getAllByRole("checkbox")
    .map((c) => c.getAttribute("aria-label") ?? "")
    .filter((name) => name !== "Select all shown rows")
    .map((name) => name.replace(/^Select /, ""));
}

async function sortBy(user: ReturnType<typeof userEvent.setup>, label: string) {
  await user.click(screen.getByRole("combobox", { name: "Sort by" }));
  await user.click(await screen.findByRole("option", { name: label }));
}

beforeAll(() => {
  // Radix Select uses pointer capture and scrolls the selected option into view; jsdom has neither.
  Element.prototype.hasPointerCapture ??= () => false;
  Element.prototype.releasePointerCapture ??= () => {};
  Element.prototype.scrollIntoView ??= () => {};
});

describe("the candidate list", () => {
  it("selects every shown row and shows the count and size in the bulk bar", async () => {
    const { user } = setup([alpha, beta, gamma]);
    expect(bar()).not.toBeInTheDocument();
    await user.click(selectAllBox());
    expect(bar()).toHaveTextContent("3 selected · 6.0 GiB");
    expect(selectAllBox()).toBeChecked();
  });

  it("selects all over the filtered rows only", async () => {
    const { user, onDelete } = setup([alpha, beta, gamma]);
    await user.type(filterBox(), "al");
    expect(shownTitles()).toEqual(["Alpha"]);
    await user.click(selectAllBox());
    await user.click(within(bar()!).getByRole("button", { name: "Delete" }));
    expect(onDelete).toHaveBeenCalledWith([alpha.key]);
  });

  it("deselects the rows a filter hides, so an action never names a hidden row", async () => {
    const { user, onDelete } = setup([alpha, beta, gamma]);
    await user.click(selectAllBox());
    await user.type(filterBox(), "be");
    expect(bar()).toHaveTextContent("1 selected · 1.0 GiB");
    await user.click(within(bar()!).getByRole("button", { name: "Delete" }));
    expect(onDelete).toHaveBeenCalledWith([beta.key]);
    // Clearing the filter shows the other rows again, not selected.
    await user.clear(filterBox());
    expect(shownTitles()).toHaveLength(3);
    expect(bar()).toHaveTextContent("1 selected · 1.0 GiB");
    expect(screen.getByRole("checkbox", { name: "Select Alpha" })).not.toBeChecked();
  });

  it("deselects the rows a reload no longer holds", async () => {
    const { user, onDelete, rerender } = setup([alpha, beta, gamma]);
    await user.click(screen.getByRole("checkbox", { name: "Select Alpha" }));
    await user.click(screen.getByRole("checkbox", { name: "Select Beta" }));
    expect(bar()).toHaveTextContent("2 selected");
    rerender([beta, gamma]);
    expect(bar()).toHaveTextContent("1 selected · 1.0 GiB");
    await user.click(within(bar()!).getByRole("button", { name: "Delete" }));
    expect(onDelete).toHaveBeenCalledWith([beta.key]);
    // A later reload that holds Alpha again does not bring its selection back.
    rerender([alpha, beta, gamma]);
    expect(bar()).toHaveTextContent("1 selected · 1.0 GiB");
    expect(screen.getByRole("checkbox", { name: "Select Alpha" })).not.toBeChecked();
  });

  it("fixes the bulk bar to the bottom of the window only while rows are selected", async () => {
    const { user } = setup([alpha, beta, gamma]);
    expect(bar()).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Select Beta" }));
    expect(bar()).toHaveClass("fixed", "bottom-0", "transition-[left]", "motion-reduce:transition-none");
    await user.click(screen.getByRole("checkbox", { name: "Select Beta" }));
    expect(bar()).not.toBeInTheDocument();
  });

  it("pads the list while the bulk bar is shown, so the last row is never behind it", async () => {
    const { user } = setup([alpha, beta, gamma]);
    const list = screen.getByTestId("candidate-list");
    expect(list.style.paddingBottom).toBe("");
    await user.click(selectAllBox());
    expect(list.style.paddingBottom).not.toBe("");
    await user.click(within(bar()!).getByRole("button", { name: "Clear selection" }));
    expect(list.style.paddingBottom).toBe("");
  });

  it("hides the bulk bar when the selection is cleared", async () => {
    const { user } = setup([alpha, beta, gamma]);
    await user.click(selectAllBox());
    await user.click(within(bar()!).getByRole("button", { name: "Clear selection" }));
    expect(bar()).not.toBeInTheDocument();
    expect(selectAllBox()).not.toBeChecked();
  });

  it("lets an action clear the selection", async () => {
    const { user } = setup([alpha, beta], {
      actions: (_selected, clear) => <button type="button" onClick={clear}>Ignore</button>,
    });
    await user.click(selectAllBox());
    await user.click(within(bar()!).getByRole("button", { name: "Ignore" }));
    expect(bar()).not.toBeInTheDocument();
  });

  it("shows the header checkbox as indeterminate when some shown rows are selected", async () => {
    const { user } = setup([alpha, beta, gamma]);
    await user.click(screen.getByRole("checkbox", { name: "Select Beta" }));
    expect(selectAllBox()).toBePartiallyChecked();
    await user.click(selectAllBox());
    expect(bar()).toHaveTextContent("3 selected");
  });

  it("sorts by size descending by default and by the chosen key and direction", async () => {
    const { user } = setup([alpha, beta, gamma]);
    expect(shownTitles()).toEqual(["Alpha", "Gamma", "Beta"]);
    await sortBy(user, "Title");
    await user.click(screen.getByRole("button", { name: "Sort ascending" }));
    expect(screen.getByRole("button", { name: "Sort descending" })).toBeInTheDocument();
    expect(shownTitles()).toEqual(["Alpha", "Beta", "Gamma"]);
    await sortBy(user, "Size");
    await user.click(screen.getByRole("button", { name: "Sort descending" }));
    expect(shownTitles()).toEqual(["Alpha", "Gamma", "Beta"]);
  });

  it("sorts a row without a watched date as the oldest", async () => {
    const { user } = setup([alpha, beta, gamma]);
    await sortBy(user, "Watched");
    expect(shownTitles()).toEqual(["Alpha", "Gamma", "Beta"]);
    await user.click(screen.getByRole("button", { name: "Sort ascending" }));
    expect(shownTitles()).toEqual(["Beta", "Gamma", "Alpha"]);
  });

  it("sorts torrents by the date the first one was added", async () => {
    const old = row("t:1", "Old", GiB, { list: "torrents", torrents: [torrent({ addedAt: "2025-01-01T00:00:00Z" })] });
    const fresh = row("t:2", "Fresh", GiB, { list: "torrents", torrents: [torrent({ addedAt: "2026-06-01T00:00:00Z" })] });
    const { user } = setup([old, fresh], { sortKeys: [{ id: "size", label: "Size" }, { id: "addedAt", label: "Added" }] });
    await sortBy(user, "Added");
    expect(shownTitles()).toEqual(["Fresh", "Old"]);
  });

  it("filters by title, year and torrent name, and by category and tracker for torrent rows only", async () => {
    const movie = row("m:1", "Delta", GiB, {
      year: 1987, torrents: [torrent({ name: "delta.release", category: "films", tracker: "tracker.example" })],
    });
    const orphan = row("t:1", "Orphan", GiB, {
      list: "torrents", torrents: [torrent({ name: "orphan.pack", category: "films", tracker: "tracker.example" })],
    });
    const { user } = setup([movie, orphan]);
    const shownFor = async (text: string) => {
      await user.clear(filterBox());
      await user.type(filterBox(), text);
      return shownTitles();
    };
    expect(await shownFor("DELTA")).toEqual(["Delta"]);
    expect(await shownFor("1987")).toEqual(["Delta"]);
    expect(await shownFor("orphan.PACK")).toEqual(["Orphan"]);
    expect(await shownFor("films")).toEqual(["Orphan"]);
    expect(await shownFor("tracker.example")).toEqual(["Orphan"]);
  });

  it("shows the empty text when there are no rows", () => {
    setup([]);
    expect(screen.getByText("Nothing to remove.")).toBeInTheDocument();
  });

  it("expands a row to show its torrents", async () => {
    const { user } = setup([alpha], { renderExpanded: (r) => <p>torrents of {r.title}</p> });
    expect(screen.queryByText("torrents of Alpha")).not.toBeInTheDocument();
    const expand = screen.getByRole("button", { name: "Show torrents of Alpha" });
    expect(expand).toHaveAttribute("aria-expanded", "false");
    await user.click(expand);
    expect(screen.getByText("torrents of Alpha")).toBeInTheDocument();
    expect(expand).toHaveAttribute("aria-expanded", "true");
    await user.click(expand);
    expect(screen.queryByText("torrents of Alpha")).not.toBeInTheDocument();
  });

  it("renders a card per row on a narrow screen, and a card's checkbox selects its row", async () => {
    vi.stubGlobal("matchMedia", (query: string): MediaQueryList => ({
      matches: false, media: query, onchange: null,
      addListener: () => {}, removeListener: () => {},
      addEventListener: () => {}, removeEventListener: () => {}, dispatchEvent: () => false,
    }));
    const { user } = setup([alpha, beta, gamma], { renderExpanded: (r) => <p>torrents of {r.title}</p> });
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText("Beta")).toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: "Select Beta" }));
    expect(bar()).toHaveTextContent("1 selected · 1.0 GiB");
    await user.click(screen.getByRole("button", { name: "Show torrents of Alpha" }));
    expect(screen.getByText("torrents of Alpha")).toBeInTheDocument();
  });

  it("names each season row with its season, so two seasons of one series are selected apart", async () => {
    const s1 = row("series:7:1", "Epsilon", GiB, { list: "series", scope: "season", season: 1 });
    const s2 = row("series:7:2", "Epsilon", 2 * GiB, { list: "series", scope: "season", season: 2 });
    const { user, onDelete } = setup([s1, s2], { renderExpanded: (r) => <p>torrents of season {r.season}</p> });
    await user.click(screen.getByRole("checkbox", { name: "Select Epsilon, season 2" }));
    expect(screen.getByRole("checkbox", { name: "Select Epsilon, season 1" })).not.toBeChecked();
    expect(bar()).toHaveTextContent("1 selected · 2.0 GiB");
    await user.click(within(bar()!).getByRole("button", { name: "Delete" }));
    expect(onDelete).toHaveBeenCalledWith([s2.key]);
    await user.click(screen.getByRole("button", { name: "Show torrents of Epsilon, season 1" }));
    expect(screen.getByText("torrents of season 1")).toBeInTheDocument();
    expect(screen.queryByText("torrents of season 2")).not.toBeInTheDocument();
  });

  it("renders a table on a wide screen", () => {
    setup([alpha]);
    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Title" })).toBeInTheDocument();
  });
});
