import { render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { describe, expect, it } from "vitest";
import { routes } from "./App";

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(<RouterProvider router={router} />);
  return router;
}

describe("the app shell", () => {
  it("sends the root to the movie list", async () => {
    const router = renderAt("/");
    expect(await screen.findByRole("heading", { level: 1, name: "Movies" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/movies");
  });

  it.each([
    ["/movies", "Movies"],
    ["/series", "Series"],
    ["/torrents", "Torrents"],
    ["/history", "History"],
    ["/settings", "Settings"],
  ])("shows the page of %s and marks its link as the current one", async (path, name) => {
    renderAt(path);
    expect(await screen.findByRole("heading", { level: 1, name })).toBeInTheDocument();
    const nav = screen.getByRole("navigation");
    const current = nav.querySelectorAll('[aria-current="page"]');
    expect(current).toHaveLength(1);
    expect(current[0]).toHaveTextContent(name);
  });

  it("names the app in the top bar", () => {
    renderAt("/movies");
    expect(screen.getByText("removarr")).toBeInTheDocument();
  });
});
